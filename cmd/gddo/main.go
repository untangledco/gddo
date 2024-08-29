// Command gddo fetches and serves documentation for Go programs.
//
// For usage, see:
//
//	gddo --help
//
// The --http flag controls which addresses gddo will listen on.
//
// The --fetch-timeout flag configures the timeout for fetching
// documentation. If the timeout is exceeded, gddo will continue fetching
// the documentation in the background. The user can refresh the page to
// check on its progress.
//
// The --refresh-interval and --max-age flags control background crawling
// of packages in the database. To enable background crawling, specify a
// refresh interval greater than zero. The --max-age flag configures how
// old a module must be before gddo will crawl it.
//
// gddo will sometimes make HTTP requests to fetch project information or
// fetch packages from a Go module proxy. The --user-agent flag configures
// the user agent that gddo will use for HTTP requests. The --request-timeout
// flag configures the timeout for roundtripping an HTTP request.
//
// gddo supports rendering documentation for multiple platforms. To
// configure the default platform, specify the --platform flag.
//
// gddo can run behind a TLS-terminating reverse proxy. In order to ensure
// that badge URIs use the correct scheme, have the reverse proxy set the
// X-Forwarded-Proto HTTP header to the desired protocol (e.g. https).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/server"
)

func main() {
	ctx := context.Background()

	cfg := &server.Config{}
	flags := cfg.FlagSet()
	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	s, err := server.New(cfg)
	if err != nil {
		log.Fatalf("error creating server: %v", err)
	}

	go func() {
		if err := serveHTTP(ctx, s, cfg); err != nil {
			log.Println(err)
		}
	}()
	var wg sync.WaitGroup
	defer wg.Wait()
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
