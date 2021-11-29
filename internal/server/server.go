package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/modcache"
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

	var source internal.Source
	if cfg.GoProxy != "" {
		source = &proxy.Source{
			URL:        cfg.GoProxy,
			HTTPClient: httpClient,
		}
	} else {
		source = &modcache.Source{
			FS: os.DirFS(cfg.GoModCache),
		}
	}

	s := &Server{
		cfg:            cfg,
		httpClient:     httpClient,
		source:         source,
		templates:      make(TemplateMap),
		importGraphSem: make(chan struct{}, 10),
	}

	if cfg.Database != "" {
		db, err := database.New(cfg.Database)
		if err != nil {
			return nil, err
		}
		s.db = db
	}

	return s, nil
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

func (s *Server) search(ctx context.Context, platform, q string) ([]internal.Package, error) {
	if s.db == nil {
		// Search requires a database
		return nil, nil
	}
	return s.db.Search(ctx, platform, q)
}

func (s *Server) packages(ctx context.Context, platform string, importPaths []string) ([]internal.Package, error) {
	if s.db == nil {
		// Populate import paths only
		var packages []internal.Package
		for _, importPath := range importPaths {
			packages = append(packages, internal.Package{
				ImportPath: importPath,
			})
		}
		return packages, nil
	}
	return s.db.Packages(ctx, platform, importPaths)
}

func (s *Server) importGraph(ctx context.Context, platform string, pkg internal.Package, level database.DepLevel) ([]internal.Package, [][2]int, error) {
	if s.db == nil {
		// Import graph requires a database
		return nil, nil, nil
	}
	return s.db.ImportGraph(ctx, platform, pkg, level)
}
