// Copyright 2017 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

// Command gddo-server is the GoPkgDoc server.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"git.sr.ht/~adnano/go-gemini"
	"git.sr.ht/~adnano/go-gemini/certificate"
)

func main() {
	ctx := context.Background()

	cfg := &Config{}
	flags := cfg.FlagSet()
	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	s, err := NewServer(cfg)
	if err != nil {
		log.Fatal("error creating server:", err)
	}
	// TODO: Crawl old modules in the background.

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
