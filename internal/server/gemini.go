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
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

func (s *Server) GeminiHandler() (gemini.Handler, error) {
	if err := parseGeminiTemplates(s.templates, s.cfg.TemplatesDir); err != nil {
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
		_, err = s.getPackage(ctx, platform, importPath, "latest")
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
			return nil
		}
		if shouldDisplayError(err) {
			// Display the error to the user
			return err
		}
	}

	pkgs, err := s.db.Search(ctx, platform, q)
	if err != nil {
		return err
	}

	s.templates.Execute(w, "search.gmi", struct {
		Query   string
		Results []database.Package
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

	pkg, err := s.getPackage(ctx, platform, importPath, version)
	if err != nil {
		return err
	}

	// The template context.
	tctx := Package{
		Package:         pkg,
		Platform:        platform,
		DefaultPlatform: s.cfg.Platform,
	}

	switch r.URL.Query().Get("view") {
	case "versions":
		s.templates.Execute(w, "versions.gmi", &tctx)

	case "imports":
		pkgs, err := s.db.Packages(ctx, platform, pkg.Imports)
		if err != nil {
			return err
		}
		tctx.Imported = pkgs
		s.templates.Execute(w, "imports.gmi", &tctx)

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

		s.templates.Execute(w, "doc.gmi", &tctx)
	}
	return nil
}

func (s *Server) serveGeminiRefresh(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	importPath := r.URL.Query().Get("import_path")
	platform := r.URL.Query().Get("platform")
	err := s.fetch(ctx, platform, importPath, proxy.LatestVersion)
	if err != nil {
		return err
	}
	w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
	return nil
}

func (s *Server) serveGeminiStdlib(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	pkgs, err := s.db.Packages(ctx, s.cfg.Platform, stdlib.Packages())
	if err != nil {
		return err
	}
	return s.templates.Execute(w, "std.gmi", struct {
		Packages []database.Package
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
