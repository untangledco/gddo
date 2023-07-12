package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"path"
	"sort"
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

	if !platforms.Valid(platform) {
		return ErrInvalidPlatform
	}

	// Check if the module is blocked
	blocked, err := s.db.IsBlocked(ctx, importPath)
	if err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}

	// Limit concurrent module fetches.
	select {
	case s.moduleFetchSem <- struct{}{}:
	default:
		return errors.New("too many fetches")
	}
	defer func() { <-s.moduleFetchSem }()

	ch := make(chan error, 1)
	go func() {
		ctx := context.Background()
		// Special case for stdlib packages
		if stdlib.Contains(importPath) {
			ch <- s.fetchModule(ctx, platform, stdlib.ModulePath, version)
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

	s.metrics.fetchesTotal.Inc()
	s.metrics.fetchesActive.Inc()
	defer s.metrics.fetchesActive.Dec()

	if err := s.fetchModule_(ctx, platform, modulePath, version); err != nil {
		s.metrics.fetchErrorsTotal.Inc()
		return err
	}
	return nil
}

func (s *Server) fetchModule_(ctx context.Context, platform, modulePath, version string) error {
	// Update the module timestamp.
	// We do this before returning any errors so that background refreshes
	// won't get stuck fetching the same broken module over and over.
	// Note that this does nothing if the module is not present in the database.
	if err := s.db.TouchModule(ctx, modulePath); err != nil {
		return err
	}

	// Retrieve module
	source, mod, err := s.sources.FindModule(modulePath, version)
	if err != nil {
		return err
	}

	if mod.ModulePath != modulePath {
		// The import paths don't match
		return ErrMismatch{
			ExpectedPath: modulePath,
			ActualPath:   mod.ModulePath,
		}
	}

	// If the module documentation is already in the database, return
	if ok, err := s.db.HasPackage(ctx, platform, mod.ModulePath, mod.Version); err != nil {
		return err
	} else if ok {
		return nil
	}

	// Retrieve packages
	fsys, err := source.Files(mod)
	if err != nil {
		return err
	}
	dirs, err := internal.ParseDirectories(fsys)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		// The module has no packages
		return ErrNoPackages
	}

	// Sort versions
	sort.Slice(mod.Versions, func(i, j int) bool {
		return semver.Compare(mod.Versions[i], mod.Versions[j]) > 0
	})

	// Fetch project information
	project, err := meta.Fetch(ctx, s.httpClient, modulePath, s.cfg.UserAgent)
	if err != nil && !errors.Is(err, meta.ErrNoInfo) {
		log.Printf("Error fetching project information for %s: %v", mod.ModulePath, err)
	}

	return s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return s.putModule(tx, platform, mod, dirs, project)
	})
}

func moduleImportPath(modulePath, dir string) string {
	if modulePath == stdlib.ModulePath && dir != "." {
		return dir
	}
	return path.Join(modulePath, dir)
}

// putModule puts a module and its associated packages in the database.
// project may be nil.
func (s *Server) putModule(tx *sql.Tx, platform string, mod *internal.Module, dirs []internal.Directory, project *meta.Project) error {
	bctx, err := buildContext(platform)
	if err != nil {
		return err
	}

	if err := s.db.PutModule(tx, mod); err != nil {
		return err
	}

	dirsMap := map[string]bool{}

	// Add packages to the database
	for _, dir := range dirs {
		importPath := moduleImportPath(mod.ModulePath, dir.Path)
		doc, err := doc.New(mod.ModulePath, &dir, bctx)
		if err != nil {
			log.Printf("Failed to build documentation for %s: %v", importPath, err)
			continue
		}
		if doc.Name == "" {
			// No documentation
			continue
		}
		pkg := internal.Package{
			Module:     *mod,
			ImportPath: importPath,
			Name:       doc.Name,
			Synopsis:   doc.Synopsis,
			Imports:    doc.Imports,
		}
		if err := s.db.PutPackage(tx, platform, pkg, &doc.Documentation); err != nil {
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

	// Add directories to the database
	for dir, isDir := range dirsMap {
		if !isDir {
			continue
		}
		importPath := moduleImportPath(mod.ModulePath, dir)
		pkg := internal.Package{
			Module:     *mod,
			ImportPath: importPath,
		}
		if err := s.db.PutPackage(tx, platform, pkg, nil); err != nil {
			return err
		}
	}

	// Update project information
	if project != nil {
		if err := s.db.PutProject(tx, *project); err != nil {
			return err
		}
	}

	return nil
}

// Refresh refreshes the oldest module in the database.
func (s *Server) Refresh(ctx context.Context) {
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
