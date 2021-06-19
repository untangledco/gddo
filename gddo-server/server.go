package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/version"
	"golang.org/x/mod/semver"
)

// The Go documentation server.
type Server struct {
	cfg         *Config
	db          *database.Database
	httpClient  *http.Client
	proxyClient *proxy.Client
	templates   TemplateMap
	statusSVG   http.Handler

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
	proxyClient := &proxy.Client{
		URL:        cfg.GoProxy,
		HTTPClient: *httpClient,
	}

	db, err := database.New(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("open database: %v", err)
	}

	return &Server{
		cfg:            cfg,
		db:             db,
		httpClient:     httpClient,
		proxyClient:    proxyClient,
		templates:      make(TemplateMap),
		importGraphSem: make(chan struct{}, 10),
	}, nil
}

// GetDoc gets the package documentation from the database or from the module
// proxy as needed.
func (s *Server) GetDoc(ctx context.Context, importPath, version string) (*database.Module, *database.Package, *doc.Package, error) {
	type result struct {
		mod *database.Module
		pkg *database.Package
		doc *doc.Package
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ctx := context.Background()
		pkg, ok, err := s.db.GetPackage(ctx, importPath, version)
		if err != nil {
			ch <- result{nil, nil, nil, err}
			return
		}
		if !ok {
			err := s.fetch(ctx, importPath, version)
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
			pkg, ok, err = s.db.GetPackage(ctx, importPath, version)
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
		}
		mod, _, err := s.db.GetModule(ctx, pkg.ModulePath)
		if err != nil {
			ch <- result{nil, nil, nil, err}
			return
		}
		// TODO: Allow the user to configure the GOOS and GOARCH
		pdoc, ok, err := s.db.GetDoc(ctx, importPath, pkg.Version, "linux", "amd64")
		if err == nil && !ok {
			err = errors.New("failed to fetch documentation")
		}
		ch <- result{&mod, &pkg, pdoc, err}
		return
	}()

	ctx, cancel := context.WithTimeout(ctx, s.cfg.GetTimeout)
	defer cancel()

	select {
	case r := <-ch:
		return r.mod, r.pkg, r.doc, r.err
	case <-ctx.Done():
		log.Printf("Serving %q as not found after timeout getting doc", importPath)
		return nil, nil, nil, ctx.Err()
	}
}

// Parses the provided request path, returning the package import path and version.
func (s *Server) parseRequestPath(ctx context.Context, path string) (string, string, error) {
	// Trim leading forward slash
	path = strings.TrimPrefix(path, "/")

	// Use version if present
	at := strings.Index(path, "@")
	if at != -1 {
		v := path[at+1:]
		importPath := path[:at]

		if !semver.IsValid(v) || version.IsPseudo(v) {
			return "", "", ErrBadVersion
		}

		return importPath, v, nil
	}

	// Use latest version
	return path, proxy.LatestVersion, nil
}

func isView(u *url.URL, key string) bool {
	rq := u.RawQuery
	return strings.HasPrefix(rq, key) &&
		(len(rq) == len(key) || rq[len(key)] == '=' || rq[len(key)] == '&')
}
