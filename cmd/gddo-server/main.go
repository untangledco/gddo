// Command gddo-server fetches and serves documentation for Go programs.
//
// For usage, see:
//
//  gddo-server --help
//
// gddo-server supports running as an HTTP server, a Gemini server, or both. The
// --http and --gemini flags control which addresses gddo-server will listen on.
// If neither are specified, gddo-server will not listen for any connections.
//
// When the --gemini flag is present, gddo-server will only accept requests for
// the hostname specified with the --hostname flag. gddo-server will also
// automatically generate TLS certificates as needed and place them in the
// directory specified with the --certs flag.
//
// Some of gddo-server's features (such as search results and import graphs)
// require a PostgreSQL database to function. The database connection URL can be
// specified with the --db flag. gddo-server also supports standalone operation
// for viewing documentation locally.
//
// If the --goproxy flag is present, gddo-server will fetch modules from the
// provided Go module proxy. Otherwise, gddo-server will load modules from the
// local Go module cache. The --modcache flag can be used to specify a different
// module cache directory.
//
// The --fetch-timeout flag configures the timeout for fetching documentation.
// If the timeout is exceeded, gddo-server will continue fetching the
// documentation in the background. The user can refresh the page to check on
// its progress.
//
// The --refresh-interval and --max-age flags control background crawling of
// packages in the database. To enable background crawling, specify a refresh
// interval greater than zero. The --max-age flag configures how old a module
// must be before gddo-server will crawl it.
//
// gddo-server will sometimes make HTTP requests to fetch project information or
// fetch packages from a Go module proxy. The --user-agent flag configures the
// user agent that gddo-server will use for HTTP requests. The --request-timeout
// flag configures the timeout for roundtripping an HTTP request.
//
// gddo-server supports rendering documentation for multiple platforms. To
// configure the default platform, specify the --platform flag.
//
// gddo-server comes bundled with assets and templates. To use your own, you can
// specify the --assets and --templates flags.
//
// gddo-server can run behind a TLS-terminating reverse proxy. In order to
// ensure that badge URIs use the correct scheme, have the reverse proxy set the
// X-Forwarded-Proto HTTP header to the desired protocol (e.g. https).
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"git.sr.ht/~adnano/go-gemini"
	"git.sr.ht/~adnano/go-gemini/certificate"
	"git.sr.ht/~sircmpwn/gddo/internal/server"
)

var (
	Version  string
	ShareDir string
)

func main() {
	ctx := context.Background()

	cfg := &server.Config{
		ShareDir: ShareDir,
	}
	flags := cfg.FlagSet()
	version := flags.Bool("v", false, "print version information")
	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	if *version {
		fmt.Println("gddo-server", Version)
		return
	}

	s, err := server.New(cfg)
	if err != nil {
		log.Fatal("error creating server:", err)
	}

	var wg sync.WaitGroup
	defer wg.Wait()

	if cfg.BindHTTP != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := serveHTTP(ctx, s, cfg); err != nil {
				log.Println(err)
			}
		}()
	}
	if cfg.BindGemini != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := serveGemini(ctx, s, cfg); err != nil {
				log.Println(err)
			}
		}()
	}
	// Refresh modules in the background
	if cfg.RefreshInterval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			refreshBackground(ctx, s, cfg)
		}()
	}
}

func serveHTTP(ctx context.Context, s *server.Server, cfg *server.Config) error {
	h, err := s.HTTPHandler()
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:         cfg.BindHTTP,
		Handler:      h,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Listen for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	errch := make(chan error, 1)
	go func() {
		errch <- srv.ListenAndServe()
	}()

	select {
	case <-c:
		return srv.Shutdown(ctx)
	case err := <-errch:
		return err
	}
}

func serveGemini(ctx context.Context, s *server.Server, cfg *server.Config) error {
	h, err := s.GeminiHandler()
	if err != nil {
		return err
	}

	certs := &certificate.Store{}
	certs.Register(cfg.Hostname)
	if err := certs.Load(cfg.CertsDir); err != nil {
		return err
	}

	srv := &gemini.Server{
		Addr:           cfg.BindGemini,
		GetCertificate: certs.Get,
		Handler:        h,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}

	// Listen for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	errch := make(chan error, 1)
	go func() {
		errch <- srv.ListenAndServe(ctx)
	}()

	select {
	case <-c:
		return srv.Shutdown(ctx)
	case err := <-errch:
		return err
	}
}

func refreshBackground(ctx context.Context, s *server.Server, cfg *server.Config) {
	// Listen for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	ticker := time.NewTicker(cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Refresh(ctx)
		case <-c:
			return
		}
	}
}
