package server

import (
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
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
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
	mux.Handle("/-/opensearch.xml", handler(s.serveOpenSearch))
	mux.Handle("/-/refresh", handler(s.serveRefresh))
	mux.Handle("/favicon.ico", staticServer.FileHandler("favicon.ico"))
	mux.Handle("/robots.txt", staticServer.FileHandler("robots.txt"))
	mux.Handle("/C", http.RedirectHandler("/cmd/cgo", http.StatusMovedPermanently))
	mux.Handle("/", handler(s.serveHome))

	cacheBusters := &httputil.CacheBusters{Handler: mux}
	if err := s.parseHTMLTemplates(s.templates, s.cfg.TemplatesDir, cacheBusters); err != nil {
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
	pkg internal.Package,
	subpkgs []internal.Package,
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
	if req.URL.RawQuery == "status.svg" {
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

	mode := NeedDocumentation | NeedProject
	switch req.Form.Get("view") {
	case "versions":
	case "platforms":
	case "imports":
		mode |= NeedImports
	case "tools":
	case "import-graph":
	default:
		mode |= NeedSubPackages
	}

	pkg, err := s.load(ctx, platform, importPath, version, mode)
	if err != nil {
		return err
	}

	pkg.Message = getFlashMessage(resp, req)

	switch req.Form.Get("view") {
	case "versions":
		return s.templates.ExecuteHTML(resp, "versions.html", http.StatusOK, &pkg)

	case "platforms":
		return s.templates.ExecuteHTML(resp, "platforms.html", http.StatusOK, &struct {
			Package
			Platforms []string
		}{pkg, platforms.Platforms()})

	case "imports":
		return s.templates.ExecuteHTML(resp, "imports.html", http.StatusOK, &pkg)

	case "tools":
		uri := fmt.Sprintf("%s/%s", getRootURL(req), importPath)
		return s.templates.ExecuteHTML(resp, "tools.html", http.StatusOK, &struct {
			Package
			URI string
		}{pkg, uri})

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
		pkgs, edges, err := s.importGraph(ctx, platform, pkg.Package, hide)
		if err != nil {
			return err
		}
		b, err := renderGraph(pkg.Package, pkgs, edges)
		if err != nil {
			return err
		}
		return s.templates.ExecuteHTML(resp, "graph.html", http.StatusOK, &struct {
			Package
			SVG  template.HTML
			Hide database.DepLevel
		}{pkg, template.HTML(b), hide})

	default:
		if play := req.Form.Get("play"); play != "" {
			u, err := s.playURL(ctx, &pkg.Documentation, req.Form.Get("play"))
			if err != nil {
				return err
			}
			http.Redirect(resp, req, u, http.StatusMovedPermanently)
			return nil
		}

		etag := s.httpEtag(pkg.Package, pkg.SubPackages, pkg.Message)
		status := http.StatusOK
		if req.Header.Get("If-None-Match") == etag {
			status = http.StatusNotModified
		}

		resp.Header().Set("Etag", etag)
		return s.templates.ExecuteHTML(resp, "doc.html", status, &pkg)
	}
}

func (s *Server) serveRefresh(resp http.ResponseWriter, req *http.Request) error {
	ctx, cancel := context.WithTimeout(req.Context(), s.cfg.FetchTimeout)
	defer cancel()

	importPath := req.Form.Get("import_path")
	platform := req.Form.Get("platform")
	err := s.fetch(ctx, platform, importPath, internal.LatestVersion)
	if err != nil {
		msg, _ := errorMessage(err)
		setFlashMessage(resp, msg)
	}
	http.Redirect(resp, req, "/"+importPath, http.StatusFound)
	return nil
}

func (s *Server) serveHome(resp http.ResponseWriter, req *http.Request) error {
	if req.URL.Path != "/" {
		return s.servePackage(resp, req)
	}

	q := strings.TrimSpace(req.Form.Get("q"))
	if q == "" {
		if s.defaultModule != "" {
			http.Redirect(resp, req, "/"+s.defaultModule, http.StatusTemporaryRedirect)
			return nil
		}
		return s.templates.ExecuteHTML(resp, "index.html", http.StatusOK, nil)
	}

	platform := req.Form.Get("platform")
	if platform == "" {
		platform = s.cfg.Platform
	}

	var msg string
	importPath, err := parseImportPath(q)
	if err == nil {
		_, err = s.load(req.Context(), platform, importPath, internal.LatestVersion, 0)
		if err == nil || errors.Is(err, ErrFetching) {
			redirect := "/" + importPath
			if platform != s.cfg.Platform {
				redirect += "?" + url.QueryEscape(platform)
			}
			http.Redirect(resp, req, redirect, http.StatusFound)
			return nil
		}
		msg, _ = errorMessage(err)
	}

	pkgs, err := s.search(req.Context(), platform, q)
	if err != nil {
		return err
	}

	// TODO: UI to choose which platform to use for searches
	return s.templates.ExecuteHTML(resp, "search.html", http.StatusOK, struct {
		Query   string
		Results []internal.Package
		Message string
	}{q, pkgs, msg})
}

func (s *Server) serveAbout(resp http.ResponseWriter, req *http.Request) error {
	uri := fmt.Sprintf("%s/%s", getRootURL(req), "rsc.io/quote")
	return s.templates.ExecuteHTML(resp, "about.html", http.StatusOK, &struct {
		URI string
	}{uri})
}

func getRootURL(req *http.Request) string {
	host := req.Host
	proto := "http"
	if req.TLS != nil {
		proto = "https"
	} else if forwardedProto := req.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		proto = forwardedProto
	}
	return fmt.Sprintf("%s://%s", proto, host)
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

func (s *Server) errorHandler(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(resp http.ResponseWriter, req *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				logPanic(req.URL, rv)
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

		msg, status := errorMessage(err)
		s.templates.ExecuteHTML(resp, "notfound.html", status,
			struct {
				Status  int
				Message string
			}{status, msg})
		if status == http.StatusInternalServerError {
			log.Printf("Error serving %s: %v", req.URL, err)
		}
	}
}
