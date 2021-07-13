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
	"sort"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	versionpkg "git.sr.ht/~sircmpwn/gddo/internal/version"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

var (
	ErrBlocked    = errors.New("blocked import path")
	ErrMismatch   = errors.New("import paths don't match")
	ErrNoPackages = errors.New("no packages found")
	ErrBadVersion = errors.New("invalid version")
)

// byVersion sorts versions from latest to oldest.
type byVersion []string

func (v byVersion) Len() int           { return len(v) }
func (v byVersion) Less(i, j int) bool { return semver.Compare(v[i], v[j]) > 0 }
func (v byVersion) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }

// fetch fetches package documentation from the module proxy and updates the database.
func (s *Server) fetch(ctx context.Context, modulePath, version string) error {
	log.Println("FETCH", modulePath)

	ch := make(chan error, 1)
	go func() {
		// Fetch in the background
		ch <- s.doFetch(context.Background(), modulePath, version)
	}()

	select {
	case err := <-ch:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) doFetch(ctx context.Context, modulePath, version string) error {
	if versionpkg.IsPseudo(version) {
		// Disallow explicitly requesting a pseudo-version.
		// Pseudo-versions can only be requested via the 'latest' version.
		return ErrBadVersion
	}

	start := time.Now().UTC()

	// Check if the module is blocked
	blocked, err := s.db.IsBlocked(ctx, modulePath)
	if err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}

	// Get latest version
	latest, err := s.latestVersion(ctx, modulePath)
	if err != nil {
		return err
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	// Only update module information if the latest version was requested.
	if version == proxy.LatestVersion {
		// Retrieve the module from the database
		mod, ok, err := s.db.GetModule(ctx, modulePath)
		if err != nil {
			return err
		}

		if ok && mod.Version == latest {
			// Module is up-to-date. Update last crawl time only.
			mod.Updated = start
			if err := s.db.PutModule(ctx, mod); err != nil {
				return err
			}
			return nil
		}

		// Retrieve the list of versions
		versions, err := s.moduleVersions(ctx, modulePath)
		if err != nil {
			return err
		}
		sort.Sort(byVersion(versions))

		// Update the module
		mod = database.Module{
			ModulePath: modulePath,
			SeriesPath: seriesPath,
			Version:    latest,
			Versions:   versions,
			Updated:    start,
		}
		if err := s.db.PutModule(ctx, mod); err != nil {
			return err
		}

		// Use latest version
		version = latest
	}

	// Retrieve module source code.
	src, err := source.Get(ctx, s.proxyClient, modulePath, version)
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
		log.Printf("Error fetching source meta for %s: %v", modulePath, err)
	}

	return nil
}

// latestVersion retrieves the latest version of a module from the module proxy.
func (s *Server) latestVersion(ctx context.Context, modulePath string) (string, error) {
	if modulePath == stdlib.ModulePath {
		return stdlib.ZipInfo(proxy.LatestVersion)
	}

	info, err := s.proxyClient.GetInfo(ctx, modulePath, proxy.LatestVersion)
	if err != nil {
		return "", err
	}
	return info.Version, nil
}

// moduleVersions retrieves a module's list of versions from the module proxy.
func (s *Server) moduleVersions(ctx context.Context, modulePath string) ([]string, error) {
	if modulePath == stdlib.ModulePath {
		return stdlib.Versions()
	}
	return s.proxyClient.ListVersions(ctx, modulePath)
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

// fetchOldest updates the oldest module in the database if necessary.
func (s *Server) fetchOldest(ctx context.Context) {
	modulePath, err := s.db.Oldest(ctx)
	if err != nil {
		log.Printf("Error retrieving oldest module: %v", err)
		return
	}
	if modulePath == "" {
		// No modules in the database yet
		return
	}
	if err := s.fetch(ctx, modulePath, proxy.LatestVersion); err != nil {
		log.Printf("Error fetching %s: %v", modulePath, err)
		return
	}
}
