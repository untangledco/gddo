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
	mux.Handle("/C", http.RedirectHandler("https://blog.golang.org/doc/articles/c_go_cgo.html", http.StatusMovedPermanently))
	mux.Handle("/", handler(s.serveHome))

	cacheBusters := &httputil.CacheBusters{Handler: mux}
	templatesDir := filepath.Join(s.cfg.AssetsDir, "templates")
	if err := parseHTMLTemplates(s.templates, templatesDir, cacheBusters); err != nil {
		return nil, err
	}
	return mux, nil
}

// httpEtag returns the package entity tag used in HTTP transactions.
func (s *Server) httpEtag(importPath, version string, flashMessages []flashMessage) string {
	b := make([]byte, 0, 128)
	b = append(b, importPath...)
	b = append(b, 0)
	b = append(b, version...)
	for _, m := range flashMessages {
		b = append(b, 0)
		b = append(b, m.ID...)
		for _, a := range m.Args {
			b = append(b, 1)
			b = append(b, a...)
		}
	}
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

	importPath := strings.TrimPrefix(req.URL.Path, "/")
	mod, pkg, pdoc, err := s.GetDoc(req.Context(), importPath)
	if err != nil {
		return err
	}

	var meta *source.Meta
	_meta, ok, err := s.db.GetMeta(req.Context(), mod.SeriesPath)
	if err != nil {
		return err
	} else if ok {
		meta = &_meta
	}

	// The template context.
	type Context struct {
		Package
		Messages []flashMessage
	}

	tpkg := Package{
		Package:    *pdoc,
		ModulePath: mod.ModulePath,
		Version:    mod.Version,
		Versions:   mod.Versions,
		CommitTime: pkg.CommitTime,
		Updated:    mod.Updated,
		Meta:       meta,
	}
	flashMessages := getFlashMessages(resp, req)

	tctx := Context{
		Package:  tpkg,
		Messages: flashMessages,
	}

	switch {
	case isView(req.URL, "imports"):
		imports, err := s.db.Packages(req.Context(), pdoc.Imports)
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

	case isView(req.URL, "importers"):
		importers, err := s.db.Importers(req.Context(), importPath)
		if err != nil {
			return err
		}
		return s.templates.ExecuteHTML(resp, "importers.html", http.StatusOK, &struct {
			Context
			Importers []database.Package
		}{tctx, importers})

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
		pkgs, edges, err := s.db.ImportGraph(req.Context(), pdoc, hide)
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
		etag := s.httpEtag(pkg.ImportPath, pkg.Version, flashMessages)
		status := http.StatusOK
		if req.Header.Get("If-None-Match") == etag {
			status = http.StatusNotModified
		}

		importCount, err := s.db.ImportCount(req.Context(), importPath)
		if err != nil {
			return err
		}
		tctx.Package.ImportCount = importCount

		subpkgs, err := s.db.SubPackages(req.Context(), pkg.ModulePath, pkg.Version, importPath)
		if err != nil {
			return err
		}
		tctx.Package.SubPackages = subpkgs

		resp.Header().Set("Etag", etag)
		return s.templates.ExecuteHTML(resp, "doc.html", status, &tctx)
	}
	return nil
}

func (s *Server) serveRefresh(resp http.ResponseWriter, req *http.Request) error {
	ctx, cancel := context.WithTimeout(req.Context(), s.cfg.GetTimeout)
	defer cancel()

	importPath := req.Form.Get("path")
	pkg, ok, err := s.db.GetPackage(ctx, importPath, proxy.LatestVersion)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	ch := make(chan error, 1)
	go func() {
		_, err := s.crawl(ctx, pkg.ModulePath)
		ch <- err
	}()
	select {
	case err = <-ch:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		// TODO: Merge this with other error handling?
		var msg string
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "Timeout encountered while fetching module source code."
		} else {
			msg = "Internal server error."
		}
		setFlashMessages(resp, []flashMessage{{
			ID:   "refresh",
			Args: []string{msg},
		}})
	}
	http.Redirect(resp, req, "/"+importPath, http.StatusFound)
	return nil
}

func (s *Server) serveStdlib(resp http.ResponseWriter, req *http.Request) error {
	mod, ok, err := s.db.GetModule(req.Context(), stdlib.ModulePath)
	if err != nil {
		return err
	} else if !ok {
		_, err = s.crawl(req.Context(), stdlib.ModulePath)
		if err != nil {
			return err
		}
		mod, _, err = s.db.GetModule(req.Context(), stdlib.ModulePath)
		if err != nil {
			return err
		}
	}
	pkgs, err := s.db.ModulePackages(req.Context(), mod.ModulePath, mod.Version)
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

	_, _, _, err := s.GetDoc(req.Context(), q)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		http.Redirect(resp, req, "/"+q, http.StatusFound)
		return nil
	}
	msgs := errorMessages(err)

	pkgs, err := s.db.Search(req.Context(), q)
	if err != nil {
		return err
	}

	return s.templates.ExecuteHTML(resp, "search.html", http.StatusOK, struct {
		Query    string
		Results  []database.Package
		Messages []flashMessage
	}{q, pkgs, msgs})
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

func errorMessages(err error) []flashMessage {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return []flashMessage{{ID: "timeout"}}
	case errors.Is(err, ErrMismatch):
		return []flashMessage{{
			ID:   "error",
			Args: []string{"Error fetching module: The provided import path doesn't match the module path present in the go.mod file."},
		}}
	case errors.Is(err, ErrNoPackages):
		return []flashMessage{{
			ID:   "error",
			Args: []string{"Error fetching module: The requested module doesn't contain any packages."},
		}}
	}
	return nil
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
			errors.Is(err, ErrNoPackages) {

			msgs := errorMessages(err)
			s.templates.Execute(resp, "notfound.html",
				struct{ Messages []flashMessage }{msgs})
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
