// Copyright 2013 The Go Authors. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd.

package main

import (
	"context"
	"errors"
	"go/build"
	"log"
	"path"
	"sort"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

var (
	ErrBlocked     = errors.New("blocked import path")
	ErrMismatch    = errors.New("import paths don't match")
	ErrNoPackages  = errors.New("no packages found")
	ErrBadVersion  = errors.New("invalid version")
	ErrInvalidPath = errors.New("invalid import path")
)

// byVersion sorts versions from latest to oldest.
type byVersion []string

func (v byVersion) Len() int           { return len(v) }
func (v byVersion) Less(i, j int) bool { return semver.Compare(v[i], v[j]) > 0 }
func (v byVersion) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }

// fetch fetches package documentation from the module proxy and updates the database.
func (s *Server) fetch(ctx context.Context, importPath, version string) error {
	// Check if the module is blocked
	blocked, err := s.db.IsBlocked(ctx, importPath)
	if err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}

	ch := make(chan error, 1)
	go func() {
		ctx := context.Background()
		// Special case for stdlib packages
		if stdlib.Contains(importPath) {
			// Get the root import path (e.g. archive/tar => archive)
			importPath = strings.SplitN(importPath, "/", 2)[0]
			ch <- s.fetchModule(ctx, importPath, version)
			return
		}
		// Loop through potential module paths
		for modulePath := importPath; modulePath != "."; modulePath = path.Dir(modulePath) {
			err := s.fetchModule(ctx, modulePath, version)
			if errors.Is(err, proxy.ErrNotFound) {
				// Try parent path
				continue
			}
			ch <- err
			break
		}
		ch <- proxy.ErrNotFound
	}()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) fetchModule(ctx context.Context, modulePath, version string) error {
	latestVersion, err := s.source.LatestVersion(ctx, modulePath)
	if err != nil {
		return err
	}
	latest := version == proxy.LatestVersion
	if latest {
		version = latestVersion
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	_, ok, err := s.db.GetModule(ctx, modulePath)
	if err != nil {
		return err
	}
	// If the module is not present in the database, add it.
	// If the latest version was requested, update the module.
	if !ok || latest {
		s.putModule(ctx, modulePath, seriesPath, latestVersion, time.Now().UTC())
	}

	// If the module documentation is already in the database, return.
	if ok, err := s.db.HasPackage(ctx, modulePath, version); err != nil {
		return err
	} else if ok {
		return nil
	}

	// Retrieve module source code.
	src, err := s.source.Get(ctx, modulePath, version)
	if err != nil {
		return err
	}
	if src.Path != modulePath {
		// The import paths don't match
		return ErrMismatch
	}
	if len(src.Packages) == 0 {
		// The module has no packages
		return ErrNoPackages
	}

	// Add packages to the database
	for _, pkg := range src.Packages {
		// TODO: Allow configuring the default GOOS,
		// and optionally let the user specify their own
		pdoc, err := doc.New(pkg, &build.Context{
			GOOS:   "linux",
			GOARCH: "amd64",
		})
		if err != nil {
			log.Println(err)
			continue
		}
		if len(pkg.Files) == 0 {
			pdoc.ImportPath = pkg.Path
		}
		if err := s.db.PutPackage(ctx, modulePath, seriesPath, src.Version, src.Time, pdoc); err != nil {
			log.Println(err)
			continue
		}
	}

	// Update meta
	if err := s.updateMeta(ctx, modulePath); err != nil {
		log.Printf("Error fetching go-source meta for %s: %v", modulePath, err)
	}

	return nil
}

// putModule puts a module in the database.
func (s *Server) putModule(ctx context.Context, modulePath, seriesPath, version string, updated time.Time) error {
	versions, err := s.source.Versions(ctx, modulePath)
	if err != nil {
		return err
	}
	sort.Sort(byVersion(versions))

	mod := database.Module{
		ModulePath: modulePath,
		SeriesPath: seriesPath,
		Version:    version,
		Versions:   versions,
		Updated:    updated,
	}
	if err := s.db.PutModule(ctx, mod); err != nil {
		return err
	}
	return nil
}

// updateMeta updates the module's go-source meta tag information.
func (s *Server) updateMeta(ctx context.Context, modulePath string) error {
	meta, err := source.FetchMeta(ctx, s.httpClient, modulePath)
	if err != nil {
		if errors.Is(err, source.ErrMetaNotFound) {
			return nil
		}
		return err
	}

	if err := s.db.PutMeta(ctx, *meta); err != nil {
		return err
	}
	return nil
}

// refreshOldest refreshes the oldest module in the database.
func (s *Server) refreshOldest(ctx context.Context) {
	modulePath, err := s.db.Oldest(ctx)
	if err != nil {
		log.Printf("Error retrieving oldest module: %v", err)
		return
	}
	if modulePath == "" {
		// No modules in the database yet
		return
	}
	log.Println("REFRESH", modulePath)
	if err := s.fetchModule(ctx, modulePath, proxy.LatestVersion); err != nil {
		log.Printf("Error refreshing %s: %v", modulePath, err)
		return
	}
}
