// Copyright 2017 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

// Command gddo-server is the GoPkgDoc server.
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
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/golang/gddo/httputil"
	"github.com/golang/gddo/internal/database"
	"github.com/golang/gddo/internal/doc"
	"github.com/golang/gddo/internal/health"
	"github.com/golang/gddo/internal/proxy"
	"github.com/golang/gddo/internal/stdlib"
)

const (
	textMIMEType = "text/plain; charset=utf-8"
	htmlMIMEType = "text/html; charset=utf-8"
)

type httpError struct {
	status int   // HTTP status code.
	err    error // Optional reason for the HTTP error.
}

func (err *httpError) Error() string {
	if err.err != nil {
		return fmt.Sprintf("status %d, reason %s", err.status, err.err.Error())
	}
	return fmt.Sprintf("Status %d", err.status)
}

// GetDoc gets the package documentation from the database or from the module
// proxy as needed.
func (s *server) GetDoc(ctx context.Context, importPath string) (*database.Module, *database.Package, *doc.Package, error) {
	type result struct {
		mod *database.Module
		pkg *database.Package
		doc *doc.Package
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ctx := context.Background()
		pkg, ok, err := s.db.GetPackage(ctx, importPath, "latest")
		if err != nil {
			ch <- result{nil, nil, nil, err}
			return
		}
		var mod database.Module
		if !ok {
			var err error
			mod, err = s.crawl(ctx, importPath)
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
			pkg, ok, err = s.db.GetPackage(ctx, importPath, "latest")
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
		} else {
			var err error
			mod, _, err = s.db.GetModule(ctx, pkg.ModulePath)
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
		}
		// TODO: Allow the user to configure the GOOS and GOARCH
		pdoc, ok, err := s.db.GetDoc(ctx, importPath, pkg.Version, "linux", "amd64")
		if err == nil && !ok {
			err = proxy.ErrNotFound
		}
		ch <- result{&mod, &pkg, pdoc, err}
		return
	}()

	timeout := s.v.GetDuration(ConfigFirstGetTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case r := <-ch:
		if r.err != nil {
			log.Printf("Error getting doc for %q: %v", importPath, r.err)
		}
		return r.mod, r.pkg, r.doc, r.err
	case <-ctx.Done():
		log.Printf("Serving %q as not found after timeout getting doc", importPath)
		return nil, nil, nil, ctx.Err()
	}
}

func templateExt(req *http.Request) string {
	if httputil.NegotiateContentType(req, []string{"text/html", "text/plain"}, "text/html") == "text/plain" {
		return ".txt"
	}
	return ".html"
}

func popularLinkReferral(req *http.Request) bool {
	return strings.HasSuffix(req.Header.Get("Referer"), "//"+req.Host+"/")
}

func isView(req *http.Request, key string) bool {
	rq := req.URL.RawQuery
	return strings.HasPrefix(rq, key) &&
		(len(rq) == len(key) || rq[len(key)] == '=' || rq[len(key)] == '&')
}

// httpEtag returns the package entity tag used in HTTP transactions.
func (s *server) httpEtag(importPath, version string, flashMessages []flashMessage) string {
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

func (s *server) servePackage(resp http.ResponseWriter, req *http.Request) error {
	if isView(req, "status.svg") {
		s.statusSVG.ServeHTTP(resp, req)
		return nil
	}

	if isView(req, "status.png") {
		s.statusPNG.ServeHTTP(resp, req)
		return nil
	}

	importPath := strings.TrimPrefix(req.URL.Path, "/")
	mod, pkg, pdoc, err := s.GetDoc(req.Context(), importPath)
	if err != nil {
		return err
	}

	flashMessages := getFlashMessages(resp, req)
	tdoc := &tdoc{
		Package:    pdoc,
		ModulePath: pkg.ModulePath,
		Version:    pkg.Version,
		Versions:   mod.Versions,
		CommitTime: pkg.CommitTime,
		Updated:    mod.Updated,
	}

	meta, ok, err := s.db.GetMeta(req.Context(), mod.SeriesPath)
	if err != nil {
		return err
	} else if ok {
		tdoc.meta = &meta
	}

	switch {
	case isView(req, "imports"):
		pkgs, err := s.db.Packages(req.Context(), pdoc.Imports)
		if err != nil {
			return err
		}
		return s.templates.execute(resp, "imports.html", http.StatusOK, nil, map[string]interface{}{
			"flashMessages": flashMessages,
			"pkgs":          pkgs,
			"pdoc":          tdoc,
			"meta":          tdoc.meta,
		})
	case isView(req, "tools"):
		proto := "http"
		if req.Host == "godocs.io" {
			proto = "https"
		}
		return s.templates.execute(resp, "tools.html", http.StatusOK, nil, map[string]interface{}{
			"flashMessages": flashMessages,
			"uri":           fmt.Sprintf("%s://%s/%s", proto, req.Host, importPath),
			"pdoc":          tdoc,
			"meta":          tdoc.meta,
		})
	case isView(req, "importers"):
		pkgs, err := s.db.Importers(req.Context(), importPath)
		if err != nil {
			return err
		}
		template := "importers.html"
		return s.templates.execute(resp, template, http.StatusOK, nil, map[string]interface{}{
			"flashMessages": flashMessages,
			"pkgs":          pkgs,
			"pdoc":          tdoc,
			"meta":          tdoc.meta,
		})
	case isView(req, "import-graph"):
		// Throttle ?import-graph requests.
		select {
		case s.importGraphSem <- struct{}{}:
		default:
			return &httpError{status: http.StatusTooManyRequests}
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
		return s.templates.execute(resp, "graph.html", http.StatusOK, nil, map[string]interface{}{
			"flashMessages": flashMessages,
			"svg":           template.HTML(b),
			"pdoc":          tdoc,
			"hide":          hide,
		})
	case isView(req, "play"):
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

		template := "dir"
		switch {
		case pdoc.IsCommand:
			template = "cmd"
		case pdoc.Name != "":
			template = "pkg"
		}
		template += templateExt(req)

		importerCount, err := s.db.ImporterCount(req.Context(), importPath)
		if err != nil {
			return err
		}

		subpkgs, err := s.db.SubPackages(req.Context(), pkg.ModulePath, pkg.Version, importPath)
		if err != nil {
			return err
		}

		return s.templates.execute(resp, template, status, http.Header{"Etag": {etag}}, map[string]interface{}{
			"flashMessages": flashMessages,
			"pkgs":          subpkgs,
			"pdoc":          tdoc,
			"meta":          tdoc.meta,
			"importerCount": importerCount,
		})
	}
	return nil
}

func (s *server) serveRefresh(resp http.ResponseWriter, req *http.Request) error {
	ctx, cancel := context.WithTimeout(req.Context(), s.v.GetDuration(ConfigGetTimeout))
	defer cancel()

	importPath := req.Form.Get("path")
	pkg, ok, err := s.db.GetPackage(ctx, importPath, "latest")
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
		setFlashMessages(resp, []flashMessage{{ID: "refresh", Args: []string{errorText(err)}}})
	}
	http.Redirect(resp, req, "/"+importPath, http.StatusFound)
	return nil
}

func (s *server) serveStdlib(resp http.ResponseWriter, req *http.Request) error {
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
	pkgs, err := s.db.ModulePackages(req.Context(), mod.Path, mod.Version)
	if err != nil {
		return err
	}
	return s.templates.execute(resp, "std.html", http.StatusOK, nil, map[string]interface{}{
		"pkgs": pkgs,
	})
	return errors.New("unimplemented")
}

func (s *server) serveHome(resp http.ResponseWriter, req *http.Request) error {
	if req.URL.Path != "/" {
		return s.servePackage(resp, req)
	}

	q := strings.TrimSpace(req.Form.Get("q"))
	if q == "" {
		return s.templates.execute(resp, "home"+templateExt(req), http.StatusOK, nil, nil)
	}

	_, _, _, err := s.GetDoc(req.Context(), q)
	if err == nil {
		http.Redirect(resp, req, "/"+q, http.StatusFound)
		return nil
	}

	pkgs, err := s.db.Search(req.Context(), q)
	if err != nil {
		return err
	}

	return s.templates.execute(resp, "results"+templateExt(req), http.StatusOK, nil,
		map[string]interface{}{"q": q, "pkgs": pkgs})
}

func (s *server) serveAbout(resp http.ResponseWriter, req *http.Request) error {
	return s.templates.execute(resp, "about.html", http.StatusOK, nil,
		map[string]interface{}{"Host": req.Host})
}

func (s *server) serveBot(resp http.ResponseWriter, req *http.Request) error {
	return s.templates.execute(resp, "bot.html", http.StatusOK, nil, nil)
}

func logError(req *http.Request, err error, rv interface{}) {
	if err != nil {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "Error serving %s: %v\n", req.URL, err)
		if rv != nil {
			fmt.Fprintln(&buf, rv)
			buf.Write(debug.Stack())
		}
		log.Print(buf.String())
	}
}

type requestCleaner struct {
	h                 http.Handler
	trustProxyHeaders bool
}

func (rc requestCleaner) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req2 := new(http.Request)
	*req2 = *req
	if rc.trustProxyHeaders {
		if s := req.Header.Get("X-Forwarded-For"); s != "" {
			req2.RemoteAddr = s
		}
	}
	req2.Body = http.MaxBytesReader(w, req.Body, 2048)
	req2.ParseForm()
	rc.h.ServeHTTP(w, req2)
}

type errorHandler struct {
	fn    func(resp http.ResponseWriter, req *http.Request) error
	errFn httputil.Error
}

func (eh errorHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	defer func() {
		if rv := recover(); rv != nil {
			err := errors.New("handler panic")
			logError(req, err, rv)
			eh.errFn(resp, req, http.StatusInternalServerError, err)
		}
	}()

	rb := new(httputil.ResponseBuffer)
	err := eh.fn(rb, req)
	if err == nil {
		rb.WriteTo(resp)
	} else if e, ok := err.(*httpError); ok {
		if e.status >= 500 {
			logError(req, err, nil)
		}
		eh.errFn(resp, req, e.status, e.err)
	} else if errors.Is(err, proxy.ErrNotFound) ||
		errors.Is(err, proxy.ErrInvalidArgument) ||
		errors.Is(err, ErrBlocked) {
		eh.errFn(resp, req, http.StatusNotFound, nil)
	} else {
		logError(req, err, nil)
		eh.errFn(resp, req, http.StatusInternalServerError, err)
	}
}

func errorText(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Timeout getting package files from the version control system."
	}
	return "Internal server error."
}

func (s *server) handleError(resp http.ResponseWriter, req *http.Request, status int, err error) {
	switch status {
	case http.StatusNotFound:
		s.templates.execute(resp, "notfound"+templateExt(req), status, nil, map[string]interface{}{
			"flashMessages": getFlashMessages(resp, req),
		})
	default:
		resp.Header().Set("Content-Type", textMIMEType)
		resp.WriteHeader(http.StatusInternalServerError)
		io.WriteString(resp, errorText(err))
	}
}

// httpsRedirectHandler redirects all requests with an X-Forwarded-Proto: http
// handler to their https equivalent.
type httpsRedirectHandler struct {
	h http.Handler
}

func (h httpsRedirectHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.Header.Get("X-Forwarded-Proto") == "http" {
		u := *req.URL
		u.Scheme = "https"
		u.Host = req.Host
		http.Redirect(resp, req, u.String(), http.StatusFound)
		return
	}
	h.h.ServeHTTP(resp, req)
}

type rootHandler []struct {
	prefix string
	h      http.Handler
}

func (m rootHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	var h http.Handler
	for _, ph := range m {
		if strings.HasPrefix(req.Host, ph.prefix) {
			h = ph.h
			break
		}
	}

	h.ServeHTTP(resp, req)
}

type server struct {
	v           *viper.Viper
	db          *database.Database
	httpClient  *http.Client
	proxyClient *proxy.Client
	templates   templateMap

	statusPNG http.Handler
	statusSVG http.Handler

	root rootHandler

	// A semaphore to limit concurrent ?import-graph requests.
	importGraphSem chan struct{}
}

func newServer(ctx context.Context, v *viper.Viper) (*server, error) {
	requestTimeout := v.GetDuration(ConfigRequestTimeout)
	var t http.RoundTripper = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   v.GetDuration(ConfigDialTimeout),
			KeepAlive: requestTimeout / 2,
		}).Dial,
		ResponseHeaderTimeout: requestTimeout / 2,
		TLSHandshakeTimeout:   requestTimeout / 2,
	}
	httpClient := &http.Client{
		Transport: t,
		Timeout:   requestTimeout,
	}
	proxyClient := &proxy.Client{
		URL:        v.GetString(ConfigGoProxy),
		HTTPClient: *httpClient,
	}

	s := &server{
		v:              v,
		httpClient:     httpClient,
		proxyClient:    proxyClient,
		importGraphSem: make(chan struct{}, 10),
	}

	assets := v.GetString(ConfigAssetsDir)
	staticServer := httputil.StaticServer{
		Dir:    assets,
		MaxAge: time.Hour,
		MIMETypes: map[string]string{
			".css": "text/css; charset=utf-8",
			".js":  "text/javascript; charset=utf-8",
		},
	}
	s.statusPNG = staticServer.FileHandler("status.png")
	s.statusSVG = staticServer.FileHandler("status.svg")

	apiHandler := func(f func(http.ResponseWriter, *http.Request) error) http.Handler {
		return requestCleaner{
			h: errorHandler{
				fn:    f,
				errFn: handleAPIError,
			},
			trustProxyHeaders: v.GetBool(ConfigTrustProxyHeaders),
		}
	}
	apiMux := http.NewServeMux()
	apiMux.Handle("/robots.txt", staticServer.FileHandler("apiRobots.txt"))
	apiMux.Handle("/search", apiHandler(s.serveAPISearch))
	apiMux.Handle("/importers/", apiHandler(s.serveAPIImporters))
	apiMux.Handle("/", apiHandler(serveAPIHome))

	mux := http.NewServeMux()
	mux.Handle("/-/site.js", staticServer.FilesHandler("site.js"))
	mux.Handle("/-/site.css", staticServer.FilesHandler("site.css"))
	mux.Handle("/-/bootstrap.min.css", staticServer.FilesHandler("bootstrap.min.css"))
	mux.Handle("/-/", http.NotFoundHandler())

	handler := func(f func(http.ResponseWriter, *http.Request) error) http.Handler {
		return requestCleaner{
			h: errorHandler{
				fn:    f,
				errFn: s.handleError,
			},
			trustProxyHeaders: v.GetBool(ConfigTrustProxyHeaders),
		}
	}
	mux.Handle("/-/about", handler(s.serveAbout))
	mux.Handle("/-/bot", handler(s.serveBot))
	mux.Handle("/std", handler(s.serveStdlib))
	mux.Handle("/-/refresh", handler(s.serveRefresh))
	mux.Handle("/about", http.RedirectHandler("/-/about", http.StatusMovedPermanently))
	mux.Handle("/favicon.ico", staticServer.FileHandler("favicon.ico"))
	mux.Handle("/robots.txt", staticServer.FileHandler("robots.txt"))
	mux.Handle("/C", http.RedirectHandler("https://blog.golang.org/doc/articles/c_go_cgo.html", http.StatusMovedPermanently))
	mux.Handle("/", handler(s.serveHome))

	ahMux := http.NewServeMux()
	ready := new(health.Handler)
	ahMux.HandleFunc("/_ah/health", health.HandleLive)
	ahMux.Handle("/_ah/ready", ready)

	mainMux := http.NewServeMux()
	mainMux.Handle("/_ah/", ahMux)
	mainMux.Handle("/", mux)

	s.root = rootHandler{
		{"api.", httpsRedirectHandler{apiMux}},
		{"", httpsRedirectHandler{mainMux}},
	}

	var err error
	cacheBusters := &httputil.CacheBusters{Handler: mux}
	s.templates, err = parseTemplates(assets, cacheBusters, v)
	if err != nil {
		return nil, err
	}
	s.db, err = database.New(v.GetString(ConfigPGServer))
	if err != nil {
		return nil, fmt.Errorf("open database: %v", err)
	}
	ready.Add(s.db)
	return s, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.root.ServeHTTP(w, r)
}

func main() {
	ctx := context.Background()
	v, err := loadConfig(ctx, os.Args)
	if err != nil {
		log.Fatal(ctx, "load config", "error", err.Error())
	}

	s, err := newServer(ctx, v)
	if err != nil {
		log.Fatal("error creating server:", err)
	}
	// TODO: Crawl old modules in the background.

	http.Handle("/", s)
	log.Fatal(http.ListenAndServe(s.v.GetString(ConfigBindAddress), s))
}
