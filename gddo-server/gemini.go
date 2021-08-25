package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"git.sr.ht/~adnano/go-gemini"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

func (s *Server) GeminiHandler() (gemini.Handler, error) {
	templatesDir := filepath.Join(s.cfg.AssetsDir, "templates")
	if err := parseGeminiTemplates(s.templates, templatesDir); err != nil {
		return nil, err
	}
	robotsTxt := filepath.Join(s.cfg.AssetsDir, "gemini-robots.txt")

	mux := &gemini.Mux{}
	mux.Handle("/-/about", geminiErrorHandler(s.serveGeminiAbout))
	mux.Handle("/-/search", geminiErrorHandler(s.serveGeminiSearch))
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
		w.WriteHeader(gemini.StatusBadRequest, "Bad request")
		return nil
	}
	q = strings.TrimSpace(q)

	importPath, err := parseImportPath(q)
	if err != nil {
		return err
	}
	_, err = s.getPackage(ctx, importPath, "latest")
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
		return nil
	}
	if errors.Is(err, ErrMismatch) || errors.Is(err, ErrNoPackages) {
		// Display the error to the user
		return err
	}

	pkgs, err := s.db.Search(ctx, q)
	if err != nil {
		w.WriteHeader(gemini.StatusTemporaryFailure, "Internal server error")
		return nil
	}

	s.templates.Execute(w, "search.gmi", struct {
		Query   string
		Results []database.Package
	}{q, pkgs})
	return nil
}

func (s *Server) serveGeminiPackage(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	if isView(r.URL, "refresh") {
		return s.serveGeminiRefresh(ctx, w, r)
	}

	importPath, version, err := s.parseRequestPath(ctx, r.URL.Path)
	if err != nil {
		return err
	}

	pkg, err := s.getPackage(ctx, importPath, version)
	if err != nil {
		return err
	}
	mod, _, err := s.db.GetModule(ctx, pkg.ModulePath)
	if err != nil {
		return err
	}
	// TODO: Configurable GOOS and GOARCH
	pdoc, err := s.db.GetDoc(ctx, pkg.ImportPath, pkg.Version, "linux", "amd64")
	if err != nil {
		return err
	}

	var meta *source.Meta
	_meta, ok, err := s.db.GetMeta(ctx, mod.SeriesPath)
	if err != nil {
		return err
	} else if ok {
		meta = &_meta
	}

	// The template context.
	tctx := Package{
		Package:    *pdoc,
		ModulePath: mod.ModulePath,
		Version:    pkg.Version,
		Versions:   mod.Versions,
		CommitTime: pkg.CommitTime,
		Updated:    mod.Updated,
		Meta:       meta,
	}

	switch {
	case isView(r.URL, "versions"):
		s.templates.Execute(w, "versions.gmi", &tctx)

	case isView(r.URL, "imports"):
		imports, err := s.db.Packages(ctx, tctx.Imports)
		if err != nil {
			return err
		}
		s.templates.Execute(w, "imports.gmi", &struct {
			Package
			Imports []database.Package
		}{tctx, imports})

	default:
		subpkgs, err := s.db.SubPackages(ctx, pkg.ModulePath, pkg.Version, importPath)
		if err != nil {
			return err
		}
		tctx.SubPackages = subpkgs

		if err := s.templates.Execute(w, "doc.gmi", &tctx); err != nil {
			log.Println(err)
		}
	}
	return nil
}

func (s *Server) serveGeminiRefresh(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.GetTimeout)
	defer cancel()

	importPath := strings.TrimPrefix(r.URL.Path, "/")
	err := s.fetch(ctx, importPath, proxy.LatestVersion)
	if err != nil {
		return err
	}
	w.WriteHeader(gemini.StatusRedirect, "/"+importPath)
	return nil
}

func (s *Server) serveGeminiStdlib(ctx context.Context, w gemini.ResponseWriter, r *gemini.Request) error {
	pkgs, err := s.db.Packages(ctx, stdlib.Packages())
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
		err := fn(ctx, w, r)
		if err == nil {
			return
		}
		switch {
		case errors.Is(err, proxy.ErrNotFound) ||
			errors.Is(err, proxy.ErrInvalidArgument) ||
			errors.Is(err, ErrBlocked):
			w.WriteHeader(gemini.StatusNotFound, "Not found")
		case errors.Is(err, context.DeadlineExceeded):
			w.WriteHeader(gemini.StatusTemporaryFailure, "This package is being fetched in the background. Feel free to refresh while we're working on it.")
		case errors.Is(err, ErrMismatch):
			w.WriteHeader(gemini.StatusNotFound, "The provided import path doesn't match the module path present in the go.mod file.")
		case errors.Is(err, ErrNoPackages):
			w.WriteHeader(gemini.StatusNotFound, "The requested module doesn't contain any packages.")
		case errors.Is(err, ErrInvalidPath):
			w.WriteHeader(gemini.StatusNotFound, "Invalid import path.")
		case errors.Is(err, ErrBadVersion):
			w.WriteHeader(gemini.StatusNotFound, "Invalid version.")
		default:
			w.WriteHeader(gemini.StatusTemporaryFailure, "Internal server error")
		}
	}
}
