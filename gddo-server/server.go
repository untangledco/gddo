package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
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
	source     source.Source

	// A semaphore to limit concurrent ?import-graph requests.
	importGraphSem chan struct{}
}

// NewServer returns a new server with the given configuration.
func NewServer(cfg *Config) (*Server, error) {
	requestTimeout := cfg.RequestTimeout
	var t http.RoundTripper = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial: (&net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: requestTimeout / 2,
		}).Dial,
		ResponseHeaderTimeout: requestTimeout / 2,
		TLSHandshakeTimeout:   requestTimeout / 2,
	}
	httpClient := &http.Client{
		Transport: t,
		Timeout:   requestTimeout,
	}
	proxySource := &source.ProxySource{
		Client: proxy.Client{
			URL:        cfg.GoProxy,
			HTTPClient: *httpClient,
		},
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

// getPackage gets the package from the database. If the package is not in the
// database, it is fetched from the module proxy.
func (s *Server) getPackage(ctx context.Context, importPath, version string) (database.Package, error) {
	type result struct {
		pkg database.Package
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ctx := context.Background()
		pkg, err := s._getPackage(ctx, importPath, version)
		ch <- result{pkg, err}
	}()

	ctx, cancel := context.WithTimeout(ctx, s.cfg.GetTimeout)
	defer cancel()

	select {
	case r := <-ch:
		return r.pkg, r.err
	case <-ctx.Done():
		log.Printf("Serving %q as not found after timeout getting doc", importPath)
		return database.Package{}, ctx.Err()
	}
}

func (s *Server) _getPackage(ctx context.Context, importPath, version string) (database.Package, error) {
	pkg, ok, err := s.db.GetPackage(ctx, importPath, version)
	if err != nil {
		return database.Package{}, err
	}
	if !ok {
		err := s.fetch(ctx, importPath, version)
		if err != nil {
			return database.Package{}, err
		}
		pkg, ok, err = s.db.GetPackage(ctx, importPath, version)
		if err != nil {
			return database.Package{}, err
		}
		if !ok {
			return database.Package{}, proxy.ErrNotFound
		}
	}
	return pkg, nil
}

// Parses the provided request path, returning the package import path and version.
func (s *Server) parseRequestPath(ctx context.Context, path string) (string, string, error) {
	// Trim leading forward slash
	importPath := strings.TrimPrefix(path, "/")
	version := proxy.LatestVersion

	// Use version if present
	at := strings.Index(importPath, "@")
	if at != -1 {
		version = importPath[at+1:]
		importPath = importPath[:at]
		if !semver.IsValid(version) {
			return "", "", ErrBadVersion
		}
	}

	// Check import path
	if err := module.CheckImportPath(importPath); err != nil {
		return "", "", ErrInvalidPath
	}

	return importPath, version, nil
}

func isView(u *url.URL, key string) bool {
	rq := u.RawQuery
	return strings.HasPrefix(rq, key) &&
		(len(rq) == len(key) || rq[len(key)] == '=' || rq[len(key)] == '&')
}

func parseImportPath(q string) (string, error) {
	// Remove leading https://
	q = strings.TrimPrefix(q, "https://")
	// Remove trailing slashes
	q = strings.TrimRight(q, "/")
	if err := module.CheckImportPath(q); err != nil {
		return "", ErrInvalidPath
	}
	return q, nil
}
