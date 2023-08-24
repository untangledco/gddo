// Package server implements the Go documentation server.
package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
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

	// A semaphore to limit concurrent module fetches.
	moduleFetchSem chan struct{}

	// Prometheus metrics
	metrics struct {
		modulesTotal     prometheus.CounterFunc
		fetchesTotal     prometheus.Counter
		fetchesActive    prometheus.Gauge
		fetchErrorsTotal prometheus.Counter
		httpPackageTotal prometheus.Counter
		httpRefreshTotal prometheus.Counter
		gmniPackageTotal prometheus.Counter
		gmniRefreshTotal prometheus.Counter
		bgRefreshTotal   prometheus.Counter
	}
}

// New returns a new server with the given configuration.
func New(cfg *Config) (*Server, error) {
	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
	}

	db, err := database.New(cfg.Database)
	if err != nil {
		return nil, err
	}
	if err := db.RegisterMetrics(prometheus.DefaultRegisterer); err != nil {
		return nil, err
	}

	s := &Server{
		cfg:            cfg,
		db:             db,
		httpClient:     httpClient,
		templates:      make(TemplateMap),
		moduleFetchSem: make(chan struct{}, 30),
	}

	s.sources = append(s.sources,
		&stdlib.RepoSource{},
		&proxy.Source{
			URL:        cfg.GoProxy,
			HTTPClient: httpClient,
		},
	)

	s.metrics.modulesTotal = promauto.NewCounterFunc(prometheus.CounterOpts{
		Name: "gddo_modules_total",
		Help: "Total number of modules indexed",
	}, func() float64 {
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
	s.metrics.httpPackageTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_http_requests_total",
		Help: "Total number of HTTP package documentation requests",
	})
	s.metrics.httpRefreshTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_http_refreshes_total",
		Help: "Total number of HTTP refresh form submissions",
	})
	s.metrics.gmniPackageTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_gemini_requests_total",
		Help: "Total number of Gemini package documentation requests",
	})
	s.metrics.gmniRefreshTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_gemini_refreshes_total",
		Help: "Total number of Gemini refresh requests",
	})
	s.metrics.bgRefreshTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gddo_background_refreshes_total",
		Help: "Total number of background module refreshes",
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
