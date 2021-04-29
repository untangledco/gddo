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
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

var (
	ErrBlocked    = errors.New("blocked import path")
	ErrMismatch   = errors.New("import paths don't match")
	ErrNoPackages = errors.New("no packages found")
)

// byVersion sorts versions from latest to oldest.
type byVersion []string

func (v byVersion) Len() int           { return len(v) }
func (v byVersion) Less(i, j int) bool { return semver.Compare(v[i], v[j]) > 0 }
func (v byVersion) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }

// crawl fetches package documentation and updates the database.
func (s *Server) crawl(ctx context.Context, modulePath string) (database.Module, error) {
	start := time.Now().UTC()

	if blocked, err := s.db.IsBlocked(ctx, modulePath); err != nil {
		return database.Module{}, err
	} else if blocked {
		return database.Module{}, ErrBlocked
	}

	// Get latest version
	var err error
	var info *proxy.VersionInfo
	if modulePath == stdlib.ModulePath {
		info = &proxy.VersionInfo{}
		info.Version, err = stdlib.ZipInfo("latest")
	} else {
		info, err = s.proxyClient.GetInfo(ctx, modulePath, "latest")
	}
	if err != nil {
		return database.Module{}, err
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	mod, ok, err := s.db.GetModule(ctx, modulePath)
	if err != nil {
		return database.Module{}, err
	}
	if ok && mod.Version == info.Version {
		// Update last crawl time
		mod.Updated = start
		if err := s.db.PutModule(ctx, mod); err != nil {
			return database.Module{}, err
		}
		return mod, nil
	}

	// Add packages to the database
	src, err := source.Get(ctx, s.proxyClient, modulePath, info.Version)
	if err != nil {
		return database.Module{}, err
	}
	if src.Path != modulePath {
		// The import paths don't match
		return database.Module{}, ErrMismatch
	}
	if len(src.Packages) == 0 {
		// The module has no packages
		return database.Module{}, ErrNoPackages
	}

	// Retrieve the list of versions
	var versions []string
	if modulePath == stdlib.ModulePath {
		versions, err = stdlib.Versions()
	} else {
		versions, err = s.proxyClient.ListVersions(ctx, modulePath)
	}
	if err != nil {
		return database.Module{}, err
	}
	sort.Sort(byVersion(versions))

	// Update the module
	mod = database.Module{
		ModulePath: modulePath,
		SeriesPath: seriesPath,
		Version:    info.Version,
		Versions:   versions,
		Updated:    start,
	}
	if err := s.db.PutModule(ctx, mod); err != nil {
		return database.Module{}, err
	}

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
		if err := s.db.PutPackage(ctx, modulePath, seriesPath, info.Version, info.Time, pdoc); err != nil {
			log.Println(err)
			continue
		}
	}

	// Fetch meta
	meta, err := source.FetchMeta(ctx, s.httpClient, modulePath)
	if err != nil {
		log.Printf("Error fetching source meta for %s: %s", modulePath, err)
	} else {
		if err := s.db.PutMeta(ctx, *meta); err != nil {
			return database.Module{}, err
		}
	}

	return mod, nil
}
