package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
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
	if err := parseHTMLTemplates(s.templates, s.cfg.TemplatesDir, cacheBusters); err != nil {
		return nil, err
	}
	return mux, nil
}

// getFlashMessage retrieves a flash message from the request and clears the flash cookie if needed.
func getFlashMessage(resp http.ResponseWriter, req *http.Request) string {
	c, err := req.Cookie("flash")
	if err == http.ErrNoCookie {
		return ""
	}
	http.SetCookie(resp, &http.Cookie{Name: "flash", Path: "/", MaxAge: -1, Expires: time.Now().Add(-100 * 24 * time.Hour)})
	if err != nil {
		return ""
	}
	p, err := base64.URLEncoding.DecodeString(c.Value)
	if err != nil {
		return ""
	}
	return string(p)
}

// setFlashMessage sets a cookie with the given flash message.
func setFlashMessage(resp http.ResponseWriter, message string) {
	value := base64.URLEncoding.EncodeToString([]byte(message))
	http.SetCookie(resp, &http.Cookie{Name: "flash", Value: value, Path: "/"})
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
	b = append(b, 0)
	b = append(b, pkg.LatestVersion...)

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

	platform := req.Form.Get("platform")
	if platform == "" {
		platform = s.cfg.Platform
	}

	pkg, err := s.getPackage(ctx, platform, importPath, version)
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

	tctx := Package{
		Package:         pkg,
		Meta:            meta,
		Message:         getFlashMessage(resp, req),
		Platform:        platform,
		DefaultPlatform: s.cfg.Platform,
	}

	switch req.Form.Get("view") {
	case "versions":
		return s.templates.ExecuteHTML(resp, "versions.html", http.StatusOK, &tctx)

	case "imports":
		pkgs, err := s.db.Packages(ctx, platform, pkg.Imports)
		if err != nil {
			return err
		}
		tctx.Imported = pkgs
		return s.templates.ExecuteHTML(resp, "imports.html", http.StatusOK, &tctx)

	case "tools":
		uri := fmt.Sprintf("%s/%s", getRootURL(req), importPath)
		return s.templates.ExecuteHTML(resp, "tools.html", http.StatusOK, &struct {
			Package
			URI string
		}{tctx, uri})

	case "import-graph":
		// Throttle import-graph requests.
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
		pkgs, edges, err := s.db.ImportGraph(ctx, platform, pkg, hide)
		if err != nil {
			return err
		}
		b, err := renderGraph(pkg, pkgs, edges)
		if err != nil {
			return err
		}
		return s.templates.ExecuteHTML(resp, "graph.html", http.StatusOK, &struct {
			Package
			SVG  template.HTML
			Hide database.DepLevel
		}{tctx, template.HTML(b), hide})

	case "play":
		doc, err := s.db.GetDocumentation(ctx, platform, pkg.ImportPath, pkg.Version)
		if err != nil {
			return err
		}
		u, err := s.playURL(ctx, &doc, req.Form.Get("play"))
		if err != nil {
			return err
		}
		http.Redirect(resp, req, u, http.StatusMovedPermanently)
		return nil

	default:
		doc, err := s.db.GetDocumentation(ctx, platform, pkg.ImportPath, pkg.Version)
		if err != nil {
			return err
		}
		tctx.Documentation = doc

		subpkgs, err := s.db.SubPackages(ctx, platform, pkg.ModulePath, pkg.Version, pkg.ImportPath)
		if err != nil {
			return err
		}
		tctx.SubPackages = subpkgs

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
	ctx, cancel := context.WithTimeout(req.Context(), s.cfg.FetchTimeout)
	defer cancel()

	importPath := req.Form.Get("import_path")
	platform := req.Form.Get("platform")
	err := s.fetch(ctx, platform, importPath, proxy.LatestVersion)
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
	pkgs, err := s.db.Packages(req.Context(), s.cfg.Platform, stdlib.Packages())
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

	platform := req.Form.Get("platform")
	if platform == "" {
		platform = s.cfg.Platform
	}

	var msg string
	importPath, err := parseImportPath(q)
	if err == nil {
		_, err = s.getPackage(req.Context(), platform, importPath, "latest")
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			redirect := "/" + importPath
			if platform != s.cfg.Platform {
				redirect += "?" + url.QueryEscape(platform)
			}
			http.Redirect(resp, req, redirect, http.StatusFound)
			return nil
		}
		msg = errorMessage(err)
	}

	pkgs, err := s.db.Search(req.Context(), platform, q)
	if err != nil {
		return err
	}

	// TODO: UI to choose which platform to use for searches
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
				resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
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

		resp.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
