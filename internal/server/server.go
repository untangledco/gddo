package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// The Go documentation server.
type Server struct {
	cfg        *Config
	db         *database.Database
	httpClient *http.Client
	templates  TemplateMap
	statusSVG  http.Handler
	source     internal.Source
	fetches    sync.Map

	// A semaphore to limit concurrent ?import-graph requests.
	importGraphSem chan struct{}
}

// New returns a new server with the given configuration.
func New(cfg *Config) (*Server, error) {
	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
	}
	proxySource := &proxy.Source{
		URL:        cfg.GoProxy,
		HTTPClient: httpClient,
	}

	db, err := database.New(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open database: %v", err)
	}

	return &Server{
		cfg:            cfg,
		db:             db,
		httpClient:     httpClient,
		source:         proxySource,
		templates:      make(TemplateMap),
		importGraphSem: make(chan struct{}, 10),
	}, nil
}

// Background refreshes modules in the background.
func (s *Server) Background(ctx context.Context) {
	for range time.Tick(s.cfg.RefreshInterval) {
		s.refreshOldest(ctx)
	}
}

// getPackage gets the package from the database. If the package is not in the
// database, it is fetched from the module proxy.
func (s *Server) getPackage(ctx context.Context, platform, importPath, version string) (database.Package, error) {
	type result struct {
		pkg database.Package
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ctx := context.Background()
		pkg, err := s._getPackage(ctx, platform, importPath, version)
		ch <- result{pkg, err}
	}()

	ctx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	select {
	case r := <-ch:
		return r.pkg, r.err
	case <-ctx.Done():
		log.Printf("Serving %q as not found after timeout getting doc", importPath)
		return database.Package{}, ctx.Err()
	}
}

func (s *Server) _getPackage(ctx context.Context, platform, importPath, version string) (database.Package, error) {
	pkg, ok, err := s.db.GetPackage(ctx, platform, importPath, version)
	if err != nil {
		return database.Package{}, err
	}
	if !ok {
		err := s.fetch(ctx, platform, importPath, version)
		if err != nil {
			return database.Package{}, err
		}
		pkg, ok, err = s.db.GetPackage(ctx, platform, importPath, version)
		if err != nil {
			return database.Package{}, err
		}
		if !ok {
			return database.Package{}, internal.ErrNotFound
		}
	}
	return pkg, nil
}

// Parses the provided request path, returning the package import path and version.
func (s *Server) parseRequestPath(ctx context.Context, path string) (string, string, error) {
	// Trim leading forward slash
	importPath := strings.TrimPrefix(path, "/")
	version := internal.LatestVersion

	// Use version if present
	at := strings.Index(importPath, "@")
	if at != -1 {
		version = importPath[at+1:]
		importPath = importPath[:at]
		if !semver.IsValid(version) {
			return "", "", internal.ErrInvalidVersion
		}
	}

	// Check import path
	if err := module.CheckImportPath(importPath); err != nil {
		return "", "", internal.ErrInvalidPath
	}

	return importPath, version, nil
}

func parseImportPath(q string) (string, error) {
	if stdlib.Contains(q) {
		return q, nil
	}
	// Remove leading https://
	q = strings.TrimPrefix(q, "https://")
	// Remove trailing slashes
	q = strings.TrimRight(q, "/")
	if err := module.CheckPath(q); err != nil {
		return "", internal.ErrInvalidPath
	}
	return q, nil
}
