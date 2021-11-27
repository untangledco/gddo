package server

import (
	"context"
	"errors"
	"log"
	"path"
	"sort"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/meta"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"golang.org/x/mod/semver"
)

// fetch fetches package documentation from the module proxy and updates the database.
func (s *Server) fetch(ctx context.Context, platform, importPath, version string) error {
	if s.db == nil {
		return nil
	}

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
			if errors.Is(err, internal.ErrNotFound) ||
				errors.Is(err, internal.ErrInvalidPath) {
				// Try parent path
				continue
			}
			ch <- err
			break
		}
		ch <- internal.ErrNotFound
	}()

	select {
	case err := <-ch:
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

	// Retrieve module
	mod, err := s.source.Module(modulePath, version)
	if err != nil {
		return err
	}
	if mod.ModulePath != modulePath {
		// The import paths don't match
		return ErrMismatch
	}
	// If the module documentation is already in the database, return
	if ok, err := s.db.HasPackage(ctx, platform, mod.ModulePath, mod.Version); err != nil {
		return err
	} else if ok {
		return nil
	}

	// Retrieve packages
	fsys, err := s.source.Files(mod)
	if err != nil {
		return err
	}
	dirs, err := internal.ParseDirectories(fsys)
	if len(dirs) == 0 {
		// The module has no packages
		return ErrNoPackages
	}

	// Sort versions
	sort.Slice(mod.Versions, func(i, j int) bool {
		return semver.Compare(mod.Versions[i], mod.Versions[j]) > 0
	})

	if err := s.db.PutModule(ctx, mod); err != nil {
		return err
	}

	dirsMap := map[string]bool{}

	// Add packages to the database
	for _, dir := range dirs {
		doc, err := doc.New(mod.ModulePath, &dir, bctx)
		if err != nil {
			log.Println(err)
			continue
		}
		if doc.Name == "" {
			// No documentation
			continue
		}
		importPath := path.Join(mod.ModulePath, dir.Path)
		if err := s.db.AddPackage(ctx, platform, importPath, mod.ModulePath,
			mod.SeriesPath, mod.Version, mod.CommitTime,
			doc.Name, doc.Synopsis, doc.Imports, &doc.Documentation); err != nil {
			return err
		}

		// Populate directory map
		dir := dir.Path
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
		importPath := path.Join(mod.ModulePath, dir)
		if err := s.db.AddPackage(ctx, platform, importPath, mod.ModulePath,
			mod.SeriesPath, mod.Version, mod.CommitTime, "", "", nil, nil); err != nil {
			return err
		}
	}

	// Update project information
	if err := s.updateProject(ctx, mod.ModulePath); err != nil {
		log.Printf("Error fetching project information for %s: %v", mod.ModulePath, err)
	}

	return nil
}

// updateProject updates the module's project information.
func (s *Server) updateProject(ctx context.Context, modulePath string) error {
	project, err := meta.Fetch(ctx, s.httpClient, modulePath, s.cfg.UserAgent)
	if err != nil {
		if errors.Is(err, meta.ErrNoInfo) {
			return nil
		}
		return err
	}

	if err := s.db.PutProject(ctx, *project); err != nil {
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
	if err := s.fetchModule(ctx, s.cfg.Platform, modulePath, internal.LatestVersion); err != nil {
		log.Printf("Error refreshing %s: %v", modulePath, err)
		return
	}
}
