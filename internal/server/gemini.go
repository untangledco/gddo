package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"git.sr.ht/~adnano/go-gemini"
	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

func (s *Server) GeminiHandler() (gemini.Handler, error) {
	if err := s.parseGeminiTemplates(s.templates, s.cfg.TemplatesDir); err != nil {
		return nil, err
	}
	robotsTxt := filepath.Join(s.cfg.AssetsDir, "gemini-robots.txt")

	mux := &gemini.Mux{}
	mux.Handle("/-/about", geminiErrorHandler(s.serveGeminiAbout))
	mux.Handle("/-/search", geminiErrorHandler(s.serveGeminiSearch))
	mux.Handle("/-/refresh", geminiErrorHandler(s.serveGeminiRefresh))
	mux.Handle("/-/", gemini.NotFoundHandler())
	mux.Handle("/std", geminiErrorHandler(s.serveGeminiStdlib))
	mux.Handle("/robots.txt", geminiFileHandler(robotsTxt, "text/plain"))
	mux.Handle("/C", gemini.StatusHandler(gemini.StatusPermanentRedirect, "/cmd/cgo"))
	mux.Handle("/", geminiErrorHandler(s.serveGeminiHome))
	return mux, nil
}

func (s *Server) serveGeminiHome(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	if r.URL.Path != "/" {
		return s.serveGeminiPackage(ctx, w, r)
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
		Results []internal.Package
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

	mode := NeedDocumentation
	switch r.URL.Query().Get("view") {
	case "versions":
	case "platforms":
	case "imports":
		mode |= NeedImports
	default:
		mode |= NeedSubPackages
	}

	pkg, err := s.load(ctx, platform, importPath, version, mode)
	if err != nil {
		return err
	}

	switch r.URL.Query().Get("view") {
	case "versions":
		s.templates.Execute(w, "versions.gmi", &pkg)

	case "platforms":
		s.templates.Execute(w, "platforms.gmi", &struct {
			Package
			Platforms []string
		}{pkg, platforms.Platforms()})

	case "imports":
		s.templates.Execute(w, "imports.gmi", &pkg)

	default:
		s.templates.Execute(w, "doc.gmi", &pkg)
	}
	return nil
}

func (s *Server) serveGeminiRefresh(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	importPath := r.URL.Query().Get("import_path")
	platform := r.URL.Query().Get("platform")
	err := s.fetch(ctx, platform, importPath, internal.LatestVersion)
	if err != nil {
		return err
	}
	w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
	return nil
}

func (s *Server) serveGeminiStdlib(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	pkgs, err := s.packages(ctx, s.cfg.Platform, stdlib.Packages())
	if err != nil {
		return err
	}
	return s.templates.Execute(w, "std.gmi", struct {
		Packages []internal.Package
	}{pkgs})
}

func geminiFileHandler(path, mediatype string) gemini.HandlerFunc {
	return func(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) {
		w.SetMediaType(mediatype)
		f, err := os.Open(path)
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
