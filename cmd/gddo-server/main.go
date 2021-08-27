// Command gddo-server is the GoPkgDoc server.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
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

	// Refresh modules in the background
	go s.Background(ctx)

	var wg sync.WaitGroup
	defer wg.Wait()
	if cfg.BindHTTP != "" {
		h, err := s.HTTPHandler()
		if err != nil {
			log.Fatal(err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := http.ListenAndServe(cfg.BindHTTP, h); err != nil {
				log.Println(err)
			}
		}()
	}
	if cfg.BindGemini != "" {
		h, err := s.GeminiHandler()
		if err != nil {
			log.Fatal(err)
		}

		certs := &certificate.Store{}
		certs.Register("*")
		if err := certs.Load(cfg.CertsDir); err != nil {
			log.Fatal(err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			gemsrv := &gemini.Server{
				Addr:           cfg.BindGemini,
				GetCertificate: certs.Get,
				Handler:        h,
				ReadTimeout:    30 * time.Second,
				WriteTimeout:   30 * time.Second,
			}
			if err := gemsrv.ListenAndServe(ctx); err != nil {
				log.Println(err)
			}
		}()
	}
}
