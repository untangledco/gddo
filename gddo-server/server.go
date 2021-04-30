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
func (s *Server) GetDoc(ctx context.Context, importPath string) (*database.Module, *database.Package, *doc.Package, error) {
	type result struct {
		mod *database.Module
		pkg *database.Package
		doc *doc.Package
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ctx := context.Background()
		pkg, ok, err := s.db.GetPackage(ctx, importPath, "latest")
		if err != nil {
			ch <- result{nil, nil, nil, err}
			return
		}
		var mod database.Module
		if !ok {
			var err error
			mod, err = s.crawl(ctx, importPath)
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
			pkg, ok, err = s.db.GetPackage(ctx, importPath, "latest")
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
		} else {
			var err error
			mod, _, err = s.db.GetModule(ctx, pkg.ModulePath)
			if err != nil {
				ch <- result{nil, nil, nil, err}
				return
			}
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
		if r.err != nil && !errors.Is(r.err, proxy.ErrNotFound) &&
			!errors.Is(r.err, proxy.ErrInvalidArgument) {
			log.Printf("Error getting doc for %q: %v", importPath, r.err)
		}
		return r.mod, r.pkg, r.doc, r.err
	case <-ctx.Done():
		log.Printf("Serving %q as not found after timeout getting doc", importPath)
		return nil, nil, nil, ctx.Err()
	}
}

func isView(u *url.URL, key string) bool {
	rq := u.RawQuery
	return strings.HasPrefix(rq, key) &&
		(len(rq) == len(key) || rq[len(key)] == '=' || rq[len(key)] == '&')
}
