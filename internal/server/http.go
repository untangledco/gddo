package server

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/httputil"
	"git.sr.ht/~sircmpwn/gddo/static"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Server) HTTPHandler() (http.Handler, error) {
	files := httputil.NewFileServer(static.FS)
	s.statusSVG = files.FileHandler("status.svg")

	mux := http.NewServeMux()
	mux.Handle("/-/site.js", files.FileHandler("site.js"))
	mux.Handle("/-/site.css", files.FileHandler("site.css"))
	mux.Handle("/-/bootstrap.min.css", files.FileHandler("bootstrap.min.css"))
	mux.Handle("/-/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		MaxRequestsInFlight: 10,
		Timeout:             10 * time.Second,
		EnableOpenMetrics:   true,
	}))
	mux.Handle("/-/", http.NotFoundHandler())

	handler := func(f func(http.ResponseWriter, *http.Request) error) http.Handler {
		return requestCleaner{
			h: s.errorHandler(f),
		}
	}
	mux.Handle("/-/about", handler(s.serveAbout))
	mux.Handle("/-/opensearch.xml", handler(s.serveOpenSearch))
	mux.Handle("/-/refresh", handler(s.serveRefresh))
	mux.Handle("/favicon.ico", files.FileHandler("favicon.ico"))
	mux.Handle("/robots.txt", files.FileHandler("robots.txt"))
	mux.Handle("/C", http.RedirectHandler("/cmd/cgo", http.StatusMovedPermanently))
	mux.Handle("/", handler(s.serveHome))

	if err := s.parseHTMLTemplates(s.templates, files); err != nil {
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
func httpEtag(pkg *Package) string {
	b := make([]byte, 0, 128)
	b = append(b, pkg.ImportPath...)
	b = append(b, 0)
	b = append(b, pkg.Version...)
	b = append(b, 0)
	b = append(b, pkg.LatestVersion...)
	b = append(b, 0)
	b = append(b, pkg.Message...)
	return fmt.Sprintf(`"%x"`, sha1.Sum(b))
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

	mode := NeedProject
	switch req.Form.Get("view") {
	case "versions":
	case "platforms":
	case "imports":
		mode |= NeedImports
	case "tools":
	case "import-graph":
	default:
		mode |= NeedDirectories
	}

	pkg, err := s.loadPackage(ctx, platform, importPath, version, mode)
	var mismatch ErrMismatch
	if errors.As(err, &mismatch) {
		http.Redirect(resp, req, "/"+mismatch.ActualPath, http.StatusFound)
		return nil
	}
	if err != nil {
		return err
	}

	pkg.Message = getFlashMessage(resp, req)

	renderer := NewRenderer(pkg, s.cfg)

	switch req.Form.Get("view") {
	case "versions":
		return renderer.ExecuteHTML(s.templates.HTML("versions.html"), resp, pkg)

	case "platforms":
		return renderer.ExecuteHTML(s.templates.HTML("platforms.html"), resp, pkg)

	case "imports":
		return renderer.ExecuteHTML(s.templates.HTML("imports.html"), resp, pkg)

	case "tools":
		uri := fmt.Sprintf("%s/%s", getRootURL(req), importPath)
		return renderer.ExecuteHTML(s.templates.HTML("tools.html"), resp, &struct {
			*Package
			URI string
		}{pkg, uri})

	default:
		if play := req.Form.Get("play"); play != "" {
			u, err := s.playURL(ctx, pkg, req.Form.Get("play"))
			if err != nil {
				return err
			}
			http.Redirect(resp, req, u, http.StatusMovedPermanently)
			return nil
		}

		s.metrics.httpPackageTotal.Inc()

		etag := httpEtag(pkg)
		if req.Header.Get("If-None-Match") == etag {
			resp.WriteHeader(http.StatusNotModified)
		}

		resp.Header().Set("Etag", etag)
		return renderer.ExecuteHTML(s.templates.HTML("doc.html"), resp, pkg)
	}
}

func (s *Server) serveRefresh(resp http.ResponseWriter, req *http.Request) error {
	s.metrics.httpRefreshTotal.Inc()
	importPath := req.Form.Get("import_path")
	platform := req.Form.Get("platform")
	err := s.fetch(req.Context(), platform, importPath, internal.LatestVersion)
	var mismatch ErrMismatch
	if errors.As(err, &mismatch) {
		http.Redirect(resp, req, "/"+mismatch.ActualPath, http.StatusFound)
		return nil
	}
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
		return s.templates.ExecuteHTML(resp, "index.html", nil)
	}

	platform := req.Form.Get("platform")
	if platform == "" {
		platform = s.cfg.Platform
	}

	var msg string
	importPath, err := parseImportPath(q)
	if err == nil {
		_, err = s.loadPackage(req.Context(), platform, importPath, internal.LatestVersion, 0)
		if err == nil || errors.Is(err, ErrFetching) {
			http.Redirect(resp, req, "/"+importPath, http.StatusFound)
			return nil
		}
		var mismatch ErrMismatch
		if errors.As(err, &mismatch) {
			http.Redirect(resp, req, "/"+mismatch.ActualPath, http.StatusFound)
			return nil
		}
		msg, _ = errorMessage(err)
	}

	pkgs, err := s.db.Search(req.Context(), platform, q)
	if err != nil {
		return err
	}

	return s.templates.ExecuteHTML(resp, "search.html", &struct {
		Query   string
		Results []database.Synopsis
		Message string
	}{q, pkgs, msg})
}

func (s *Server) serveAbout(resp http.ResponseWriter, req *http.Request) error {
	uri := fmt.Sprintf("%s/%s", getRootURL(req), "archive/tar")
	return s.templates.ExecuteHTML(resp, "about.html", &struct {
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
	return s.templates.Execute(resp, "opensearch.xml", root)
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
		if errors.Is(err, context.Canceled) {
			// Request was cancelled
			return
		}

		msg, status := errorMessage(err)
		resp.WriteHeader(status)
		s.templates.ExecuteHTML(resp, "notfound.html", &struct {
			Status  int
			Message string
		}{status, msg})
		if status == http.StatusInternalServerError {
			log.Printf("Error serving %s: %v", req.URL, err)
		}
	}
}
