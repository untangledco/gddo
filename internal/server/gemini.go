package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"git.sr.ht/~adnano/go-gemini"
	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/static"
)

func (s *Server) GeminiHandler() (gemini.Handler, error) {
	if err := s.parseGeminiTemplates(s.templates); err != nil {
		return nil, err
	}

	mux := &gemini.Mux{}
	mux.Handle("/-/about", geminiErrorHandler(s.serveGeminiAbout))
	mux.Handle("/-/search", geminiErrorHandler(s.serveGeminiSearch))
	mux.Handle("/-/refresh", geminiErrorHandler(s.serveGeminiRefresh))
	mux.Handle("/-/", gemini.NotFoundHandler())
	mux.Handle("/robots.txt", geminiFileHandler("gemini-robots.txt", "text/plain"))
	mux.Handle("/C", gemini.StatusHandler(gemini.StatusPermanentRedirect, "/cmd/cgo"))
	mux.Handle("/", geminiErrorHandler(s.serveGeminiHome))
	return mux, nil
}

func (s *Server) serveGeminiHome(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	if r.URL.Path != "/" {
		return s.serveGeminiPackage(ctx, w, r)
	}
	if s.defaultModule != "" {
		w.WriteHeader(gemini.StatusRedirect, "/"+s.defaultModule)
		return nil
	}
	return s.templates.Execute(w, "index.gmi", nil)
}

func (s *Server) serveGeminiAbout(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	return s.templates.Execute(w, "about.gmi", nil)
}

func (s *Server) serveGeminiSearch(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	if len(r.URL.RawQuery) == 0 {
		w.WriteHeader(gemini.StatusInput, "Search query")
		return nil
	}

	q, err := gemini.QueryUnescape(r.URL.RawQuery)
	if err != nil {
		w.WriteHeader(gemini.StatusBadRequest, "Bad request.")
		return nil
	}
	q = strings.TrimSpace(q)

	// TODO: Some way of specifying the platform in Gemini searches
	platform := s.cfg.Platform

	importPath, err := parseImportPath(q)
	if err == nil {
		_, err = s.load(ctx, platform, importPath, internal.LatestVersion, 0)
		if err == nil || errors.Is(err, ErrFetching) {
			w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
			return nil
		}
		var mismatch ErrMismatch
		if errors.As(err, &mismatch) {
			w.WriteHeader(gemini.StatusRedirect, "/"+mismatch.ActualPath)
			return nil
		}
		if shouldDisplayError(err) {
			// Display the error to the user
			return err
		}
	}

	pkgs, err := s.search(ctx, platform, q)
	if err != nil {
		return err
	}

	s.templates.Execute(w, "search.gmi", struct {
		Query   string
		Results []database.PackageSynopsis
	}{q, pkgs})
	return nil
}

func (s *Server) serveGeminiPackage(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	importPath, version, err := s.parseRequestPath(ctx, r.URL.Path)
	if err != nil {
		return err
	}

	platform := r.URL.Query().Get("platform")
	if platform == "" {
		platform = s.cfg.Platform
	}

	mode := LoadMode(0)
	switch r.URL.Query().Get("view") {
	case "versions":
	case "platforms":
	case "imports":
		mode |= NeedImports
	default:
		mode |= NeedSubPackages
	}

	pkg, err := s.load(ctx, platform, importPath, version, mode)
	var mismatch ErrMismatch
	if errors.As(err, &mismatch) {
		w.WriteHeader(gemini.StatusRedirect, "/"+mismatch.ActualPath)
		return nil
	}
	if err != nil {
		return err
	}

	renderer := NewRenderer(pkg, s.cfg)

	switch r.URL.Query().Get("view") {
	case "versions":
		renderer.ExecuteGemini(s.templates.Text("versions.gmi"), w, pkg)

	case "platforms":
		renderer.ExecuteGemini(s.templates.Text("platforms.gmi"), w, pkg)

	case "imports":
		renderer.ExecuteGemini(s.templates.Text("imports.gmi"), w, pkg)

	default:
		renderer.ExecuteGemini(s.templates.Text("doc.gmi"), w, pkg)
	}
	return nil
}

func (s *Server) serveGeminiRefresh(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	importPath := r.URL.Query().Get("import_path")
	platform := r.URL.Query().Get("platform")
	err := s.fetch(ctx, platform, importPath, internal.LatestVersion)
	var mismatch ErrMismatch
	if errors.As(err, &mismatch) {
		w.WriteHeader(gemini.StatusRedirect, "/"+mismatch.ActualPath)
		return nil
	}
	if err != nil {
		return err
	}
	w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
	return nil
}

func geminiFileHandler(path, mediatype string) gemini.HandlerFunc {
	return func(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) {
		w.SetMediaType(mediatype)
		f, err := static.FS.Open(path)
		if err != nil {
			w.WriteHeader(gemini.StatusTemporaryFailure, "Internal server error.")
			return
		}
		defer f.Close()
		io.Copy(w, f)
	}
}

func geminiErrorHandler(fn func(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error) gemini.HandlerFunc {
	return func(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				logPanic(r.URL, rv)
				w.WriteHeader(gemini.StatusTemporaryFailure, "Internal server error.")
			}
		}()

		err := fn(ctx, w, r)
		if err == nil {
			return
		}
		if errors.Is(err, context.Canceled) {
			// Request was cancelled
			return
		}

		msg, httpStatus := errorMessage(err)
		status := gemini.StatusTemporaryFailure
		if msg == "" {
			msg = "Not found."
			status = gemini.StatusNotFound
		}
		w.WriteHeader(status, msg)
		if httpStatus == http.StatusInternalServerError {
			log.Printf("Error serving %s: %v", r.URL, err)
		}
	}
}
