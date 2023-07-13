// Package server implements the Go documentation server.
package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/modcache"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	sources    internal.SourceList
	fetches    sync.Map

	// The module to serve instead of the homepage (if any)
	defaultModule string

	// A semaphore to limit concurrent module fetches.
	moduleFetchSem chan struct{}

	// A semaphore to limit concurrent ?import-graph requests.
	importGraphSem chan struct{}

	// Prometheus metrics
	metrics struct {
		modulesTotal       prometheus.CounterFunc
		fetchesTotal       prometheus.Counter
		fetchesActive      prometheus.Gauge
		fetchErrorsTotal   prometheus.Counter
		importGraphsTotal  prometheus.Counter
		importGraphsActive prometheus.Gauge
	}
}

// New returns a new server with the given configuration.
func New(cfg *Config) (*Server, error) {
	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
	}

	s := &Server{
		cfg:            cfg,
		httpClient:     httpClient,
		templates:      make(TemplateMap),
		moduleFetchSem: make(chan struct{}, 30),
		importGraphSem: make(chan struct{}, 10),
	}

	if cfg.GoProxy != "" {
		s.sources = append(s.sources,
			&stdlib.RepoSource{},
			&proxy.Source{
				URL:        cfg.GoProxy,
				HTTPClient: httpClient,
			},
		)
	} else {
		// Serve the current directory
		if dir, err := internal.DirectorySource("."); err != nil {
			return nil, fmt.Errorf("current directory contains invalid module: %w", err)
		} else if dir != nil {
			// A valid go.mod file was found
			s.sources = append(s.sources, dir)
			s.defaultModule = dir.Mod.ModulePath
		}

		s.sources = append(s.sources,
			&stdlib.LocalSource{},
			&modcache.Source{
				FS: os.DirFS(cfg.GoModCache),
			},
		)
	}

	if cfg.Database != "" {
		db, err := database.New(cfg.Database)
		if err != nil {
			return nil, err
		}
		if err := db.RegisterMetrics(prometheus.DefaultRegisterer); err != nil {
			return nil, err
		}
		s.db = db
	}

	s.metrics.modulesTotal = promauto.NewCounterFunc(prometheus.CounterOpts{
		Name: "gddo_modules_total",
		Help: "Total number of modules indexed",
	}, func() float64 {
		if s.db == nil {
			return 0
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		count, err := s.db.Modules(ctx)
		if err != nil {
			return 0
		}
		return float64(count)
	})
	s.metrics.fetchesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gddo_fetches_total",
		Help: "Total number of module fetches",
	})
	s.metrics.fetchesActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gddo_fetches_active",
		Help: "Number of active module fetches",
	})
	s.metrics.fetchErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_fetch_errors_total",
		Help: "Total number of module fetch errors",
	})
	s.metrics.importGraphsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_import_graphs_total",
		Help: "Total number of import graph requests",
	})
	s.metrics.importGraphsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gddo_import_graphs_active",
		Help: "Number of active import graph requests",
	})

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

func (s *Server) importGraph(ctx context.Context, platform string, pkg internal.Package, level database.DepLevel) ([]internal.Package, [][2]int, error) {
	if s.db == nil {
		// Import graph requires a database
		return nil, nil, nil
	}
	return s.db.ImportGraph(ctx, platform, pkg, level)
}
