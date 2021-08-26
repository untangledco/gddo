package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

const (
	textMIMEType = "text/plain; charset=utf-8"
	htmlMIMEType = "text/html; charset=utf-8"
)

func (s *Server) HTTPHandler() (http.Handler, error) {
	staticServer := httputil.StaticServer{
		Dir:    s.cfg.AssetsDir,
		MaxAge: time.Hour,
	}
	s.statusSVG = staticServer.FileHandler("status.svg")

	mux := http.NewServeMux()
	mux.Handle("/-/site.js", staticServer.FileHandler("site.js"))
	mux.Handle("/-/site.css", staticServer.FileHandler("site.css"))
	mux.Handle("/-/bootstrap.min.css", staticServer.FileHandler("bootstrap.min.css"))
	mux.Handle("/-/", http.NotFoundHandler())

	handler := func(f func(http.ResponseWriter, *http.Request) error) http.Handler {
		return requestCleaner{
			h: s.errorHandler(f),
		}
	}
	mux.Handle("/-/about", handler(s.serveAbout))
	mux.Handle("/-/bot", handler(s.serveBot))
	mux.Handle("/-/opensearch.xml", handler(s.serveOpenSearch))
	mux.Handle("/std", handler(s.serveStdlib))
	mux.Handle("/-/refresh", handler(s.serveRefresh))
	mux.Handle("/favicon.ico", staticServer.FileHandler("favicon.ico"))
	mux.Handle("/robots.txt", staticServer.FileHandler("robots.txt"))
	mux.Handle("/C", http.RedirectHandler("https://go.dev/blog/cgo", http.StatusMovedPermanently))
	mux.Handle("/", handler(s.serveHome))

	cacheBusters := &httputil.CacheBusters{Handler: mux}
	templatesDir := filepath.Join(s.cfg.AssetsDir, "templates")
	if err := parseHTMLTemplates(s.templates, templatesDir, cacheBusters); err != nil {
		return nil, err
	}
	return mux, nil
}

// httpEtag returns the package entity tag used in HTTP transactions.
func (s *Server) httpEtag(
	pkg database.Package,
	subpkgs []database.Package,
	msg string,
) string {
	b := make([]byte, 0, 128)
	b = append(b, pkg.ImportPath...)
	b = append(b, 0)
	b = append(b, pkg.Version...)

	for _, subpkg := range subpkgs {
		b = append(b, 0)
		b = append(b, subpkg.ImportPath...)
		b = append(b, 0)
		b = append(b, subpkg.Synopsis...)
	}

	b = append(b, 0)
	b = append(b, msg...)

	h := md5.New()
	h.Write(b)
	b = h.Sum(b[:0])
	return fmt.Sprintf("\"%x\"", b)
}

func (s *Server) servePackage(resp http.ResponseWriter, req *http.Request) error {
	if isView(req.URL, "status.svg") {
		s.statusSVG.ServeHTTP(resp, req)
		return nil
	}

	ctx := req.Context()
	importPath, version, err := s.parseRequestPath(ctx, req.URL.Path)
	if err != nil {
		return err
	}

	pkg, err := s.getPackage(ctx, importPath, version)
	if err != nil {
		return err
	}
	// TODO: Configurable GOOS and GOARCH
	pdoc, err := s.db.GetDoc(ctx, pkg.ImportPath, pkg.Version, "linux", "amd64")
	if err != nil {
		return err
	}

	var meta *source.Meta
	_meta, ok, err := s.db.GetMeta(ctx, pkg.SeriesPath)
	if err != nil {
		return err
	} else if ok {
		meta = &_meta
	}

	// The template context.
	type Context struct {
		Package
		Message string
	}

	tpkg := Package{
		Package:    *pdoc,
		ModulePath: pkg.ModulePath,
		Version:    pkg.Version,
		Versions:   pkg.Versions,
		CommitTime: pkg.CommitTime,
		Updated:    pkg.Updated,
		Meta:       meta,
	}

	tctx := Context{
		Package: tpkg,
		Message: getFlashMessage(resp, req),
	}

	switch {
	case isView(req.URL, "versions"):
		return s.templates.ExecuteHTML(resp, "versions.html", http.StatusOK, &tctx)

	case isView(req.URL, "imports"):
		imports, err := s.db.Packages(ctx, pdoc.Imports)
		if err != nil {
			return err
		}
		return s.templates.ExecuteHTML(resp, "imports.html", http.StatusOK, &struct {
			Context
			Imports []database.Package
		}{tctx, imports})

	case isView(req.URL, "tools"):
		uri := fmt.Sprintf("%s/%s", getRootURL(req), importPath)
		return s.templates.ExecuteHTML(resp, "tools.html", http.StatusOK, &struct {
			Context
			URI string
		}{tctx, uri})

	case isView(req.URL, "import-graph"):
		// Throttle ?import-graph requests.
		select {
		case s.importGraphSem <- struct{}{}:
		default:
			return errors.New("too many requests")
		}
		defer func() { <-s.importGraphSem }()

		hide := database.ShowAllDeps
		switch req.Form.Get("hide") {
		case "1":
			hide = database.HideStandardDeps
		case "2":
			hide = database.HideStandardAll
		}
		pkgs, edges, err := s.db.ImportGraph(ctx, pdoc, hide)
		if err != nil {
			return err
		}
		b, err := renderGraph(pdoc, pkgs, edges)
		if err != nil {
			return err
		}
		return s.templates.ExecuteHTML(resp, "graph.html", http.StatusOK, &struct {
			Context
			SVG  template.HTML
			Hide database.DepLevel
		}{tctx, template.HTML(b), hide})

	case isView(req.URL, "play"):
		u, err := s.playURL(pdoc, req.Form.Get("play"))
		if err != nil {
			return err
		}
		http.Redirect(resp, req, u, http.StatusMovedPermanently)
		return nil

	default:
		subpkgs, err := s.db.SubPackages(ctx, pkg.ModulePath, pkg.Version, importPath)
		if err != nil {
			return err
		}
		tctx.Package.SubPackages = subpkgs

		etag := s.httpEtag(pkg, subpkgs, tctx.Message)
		status := http.StatusOK
		if req.Header.Get("If-None-Match") == etag {
			status = http.StatusNotModified
		}

		resp.Header().Set("Etag", etag)
		return s.templates.ExecuteHTML(resp, "doc.html", status, &tctx)
	}
}

func (s *Server) serveRefresh(resp http.ResponseWriter, req *http.Request) error {
	ctx, cancel := context.WithTimeout(req.Context(), s.cfg.GetTimeout)
	defer cancel()

	importPath := req.Form.Get("path")
	err := s.fetch(ctx, importPath, proxy.LatestVersion)
	if err != nil {
		// TODO: Merge this with other error handling?
		var msg string
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "Timeout encountered while fetching module source code."
		} else {
			msg = "Internal server error."
		}
		setFlashMessage(resp, fmt.Sprintf("Error refreshing package: %s", msg))
	}
	http.Redirect(resp, req, "/"+importPath, http.StatusFound)
	return nil
}

func (s *Server) serveStdlib(resp http.ResponseWriter, req *http.Request) error {
	pkgs, err := s.db.Packages(req.Context(), stdlib.Packages())
	if err != nil {
		return err
	}
	return s.templates.ExecuteHTML(resp, "std.html", http.StatusOK, struct {
		Packages []database.Package
	}{pkgs})
}

func (s *Server) serveHome(resp http.ResponseWriter, req *http.Request) error {
	if req.URL.Path != "/" {
		return s.servePackage(resp, req)
	}

	q := strings.TrimSpace(req.Form.Get("q"))
	if q == "" {
		return s.templates.ExecuteHTML(resp, "index.html", http.StatusOK, nil)
	}

	var msg string
	importPath, err := parseImportPath(q)
	if err == nil {
		_, err = s.getPackage(req.Context(), importPath, "latest")
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			http.Redirect(resp, req, "/"+importPath, http.StatusFound)
			return nil
		}
		msg = errorMessage(err)
	}

	pkgs, err := s.db.Search(req.Context(), q)
	if err != nil {
		return err
	}

	return s.templates.ExecuteHTML(resp, "search.html", http.StatusOK, struct {
		Query   string
		Results []database.Package
		Message string
	}{q, pkgs, msg})
}

func (s *Server) serveAbout(resp http.ResponseWriter, req *http.Request) error {
	return s.templates.ExecuteHTML(resp, "about.html", http.StatusOK, nil)
}

func (s *Server) serveBot(resp http.ResponseWriter, req *http.Request) error {
	return s.templates.ExecuteHTML(resp, "bot.html", http.StatusOK, nil)
}

func getRootURL(req *http.Request) string {
	if req.TLS != nil {
		return fmt.Sprintf("https://%s", strings.TrimSuffix(req.Host, ":443"))
	}
	return fmt.Sprintf("http://%s", strings.TrimSuffix(req.Host, ":80"))
}

func (s *Server) serveOpenSearch(resp http.ResponseWriter, req *http.Request) error {
	resp.Header().Set("Content-Type", "application/opensearchdescription+xml")
	root := getRootURL(req)
	return s.templates.ExecuteHTTP(resp, "opensearch.xml", http.StatusOK, root)
}

type requestCleaner struct {
	h http.Handler
}

func (rc requestCleaner) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req2 := new(http.Request)
	*req2 = *req
	req2.Body = http.MaxBytesReader(w, req.Body, 2048)
	req2.ParseForm()
	rc.h.ServeHTTP(w, req2)
}

func errorMessage(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "This package is being fetched in the background. Feel free to refresh while we're working on it."
	case errors.Is(err, ErrMismatch):
		return "Error fetching module: The provided import path doesn't match the module path present in the go.mod file."
	case errors.Is(err, ErrNoPackages):
		return "Error fetching module: The requested module doesn't contain any packages."
	case errors.Is(err, ErrInvalidPath):
		return "Error fetching module: Invalid import path."
	case errors.Is(err, ErrBadVersion):
		return "Error fetching module: Invalid version."
	}
	return ""
}

func (s *Server) errorHandler(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(resp http.ResponseWriter, req *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				err := errors.New("handler panic")
				logError(req, err, rv)
				resp.Header().Set("Content-Type", textMIMEType)
				resp.WriteHeader(http.StatusInternalServerError)
				io.WriteString(resp, "Internal server error.")
			}
		}()

		rb := new(httputil.ResponseBuffer)
		err := fn(rb, req)
		if err == nil {
			rb.WriteTo(resp)
			return
		}

		if errors.Is(err, proxy.ErrNotFound) ||
			errors.Is(err, proxy.ErrInvalidArgument) ||
			errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrBlocked) ||
			errors.Is(err, ErrMismatch) ||
			errors.Is(err, ErrNoPackages) ||
			errors.Is(err, ErrBadVersion) ||
			errors.Is(err, ErrInvalidPath) {

			msg := errorMessage(err)
			s.templates.Execute(resp, "notfound.html",
				struct{ Message string }{msg})
			return
		}

		resp.Header().Set("Content-Type", textMIMEType)
		resp.WriteHeader(http.StatusInternalServerError)
		io.WriteString(resp, "Internal server error.")
		logError(req, err, nil)
	}
}

func logError(req *http.Request, err error, rv interface{}) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Error serving %s: %v\n", req.URL, err)
	if rv != nil {
		fmt.Fprintln(&buf, rv)
		buf.Write(debug.Stack())
	}
	log.Print(buf.String())
}
