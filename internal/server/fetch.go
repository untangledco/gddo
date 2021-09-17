package server

import (
	"context"
	"errors"
	"log"
	"path"
	"sort"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/source"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// byVersion sorts versions from latest to oldest.
type byVersion []string

func (v byVersion) Len() int           { return len(v) }
func (v byVersion) Less(i, j int) bool { return semver.Compare(v[i], v[j]) > 0 }
func (v byVersion) Swap(i, j int)      { v[i], v[j] = v[j], v[i] }

// fetch fetches package documentation from the module proxy and updates the database.
func (s *Server) fetch(ctx context.Context, platform, importPath, version string) error {
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
			modulePath := strings.SplitN(importPath, "/", 2)[0]
			ch <- s.fetchModule(ctx, platform, modulePath, version)
			return
		}
		// Loop through potential module paths
		for modulePath := importPath; modulePath != "."; modulePath = path.Dir(modulePath) {
			err := s.fetchModule(ctx, platform, modulePath, version)
			if errors.Is(err, proxy.ErrNotFound) ||
				errors.Is(err, proxy.ErrInvalidPath) {
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
		if err == context.DeadlineExceeded {
			return ErrFetching
		}
		return err
	case <-ctx.Done():
		return ErrFetching
	}
}

func (s *Server) fetchModule(ctx context.Context, platform, modulePath, version string) error {
	type fetchKey struct {
		platform, modulePath, version string
	}

	key := fetchKey{platform, modulePath, version}
	if _, ok := s.fetches.LoadOrStore(key, struct{}{}); ok {
		return ErrFetching
	}
	defer s.fetches.Delete(key)

	bctx, err := platforms.Parse(platform)
	if err != nil {
		return err
	}

	latestVersion, err := s.source.LatestVersion(ctx, modulePath)
	if err != nil {
		return err
	}
	if version == proxy.LatestVersion {
		version = latestVersion
	}

	versions, err := s.source.Versions(ctx, modulePath)
	if err != nil {
		return err
	}
	sort.Sort(byVersion(versions))

	seriesPath, _, _ := module.SplitPathVersion(modulePath)
	if err := s.db.PutModule(ctx, modulePath, seriesPath, latestVersion, versions); err != nil {
		return err
	}

	// If the module documentation is already in the database, return.
	if ok, err := s.db.HasPackage(ctx, platform, modulePath, version); err != nil {
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

	dirsMap := map[string]bool{}

	// Add packages to the database
	for _, pkg := range src.Packages {
		doc, err := doc.New(pkg, bctx)
		if err != nil {
			log.Println(err)
			continue
		}
		if doc.Name == "" {
			// No documentation
			continue
		}
		if err := s.db.AddPackage(ctx, platform, pkg.Path, modulePath, seriesPath, src.Version, src.Time, doc); err != nil {
			log.Println(err)
			continue
		}

		// Populate directory map
		dir := strings.TrimPrefix(pkg.Path, src.Path)
		dir = strings.TrimPrefix(dir, "/")
		dir = path.Clean(dir)
		dirsMap[dir] = false
		for dir != "." {
			dir = path.Dir(dir)
			_, ok := dirsMap[dir]
			if ok {
				break
			}
			dirsMap[dir] = true
		}
	}

	for dir, isDir := range dirsMap {
		if !isDir {
			continue
		}
		// Add the directory to the database
		importPath := path.Join(src.Path, dir)
		if err := s.db.AddPackage(ctx, platform, importPath, modulePath, seriesPath, src.Version, src.Time, nil); err != nil {
			return err
		}
	}

	// Update meta
	if err := s.updateMeta(ctx, modulePath); err != nil {
		log.Printf("Error fetching go-source meta for %s: %v", modulePath, err)
	}

	return nil
}

// updateMeta updates the module's go-source meta tag information.
func (s *Server) updateMeta(ctx context.Context, modulePath string) error {
	meta, err := source.FetchMeta(ctx, s.httpClient, modulePath, s.cfg.UserAgent)
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
	modulePath, timestamp, err := s.db.Oldest(ctx)
	if err != nil {
		log.Printf("Error retrieving oldest module: %v", err)
		return
	}
	if modulePath == "" {
		// No modules in the database yet
		return
	}
	if time.Now().Sub(timestamp) < s.cfg.MaxAge {
		return
	}
	log.Println("REFRESH", modulePath)
	if err := s.fetchModule(ctx, s.cfg.Platform, modulePath, proxy.LatestVersion); err != nil {
		log.Printf("Error refreshing %s: %v", modulePath, err)
		return
	}
}
