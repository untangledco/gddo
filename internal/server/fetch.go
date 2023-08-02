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
	"git.sr.ht/~sircmpwn/gddo/internal/autodiscovery"
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
		// The module paths don't match
		return ErrMismatch{
			ExpectedPath: modulePath,
			ActualPath:   mod.ModulePath,
		}
	}

	if err := s.db.PutModule(ctx, mod); err != nil {
		return err
	}

	// Update project information
	lastUpdated, err := s.db.ProjectUpdated(ctx, modulePath)
	if err != nil {
		return err
	}
	if time.Since(lastUpdated) > 5*time.Minute {
		project, err := autodiscovery.Fetch(ctx, s.httpClient, mod.SeriesPath, s.cfg.UserAgent)
		if err != nil {
			log.Printf("Error fetching project information for %s: %v", modulePath, err)
		}
		if project != nil {
			if err := s.db.PutProject(ctx, modulePath, project); err != nil {
				return err
			}
		}
	}

	// If the packages are already in the database, return
	if ok, err := s.db.HasPackage(ctx, platform, modulePath, mod.Version); err != nil {
		return err
	} else if ok {
		return nil
	}

	// Retrieve packages
	fsys, err := source.Files(mod)
	if err != nil {
		return err
	}
	pkgs, err := parsePackages(platform, modulePath, fsys)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		// The module has no packages
		return ErrNoPackages
	}

	// Sort versions
	sort.Slice(mod.Versions, func(i, j int) bool {
		return semver.Compare(mod.Versions[i], mod.Versions[j]) > 0
	})

	return s.db.WithTx(ctx, nil, func(tx *sql.Tx) error {
		return s.putPackages(tx, platform, mod, pkgs)
	})
}

// putPackages puts the packages for a given module in the database.
func (s *Server) putPackages(tx *sql.Tx, platform string, mod *internal.Module, pkgs map[string]*internal.Package) error {
	for importPath, pkg := range pkgs {
		// Encode source files before rendering documentation, since
		// doc.New overwrites the AST.
		source, err := pkg.FastEncode()
		if err != nil {
			return err
		}

		// TODO: Handle empty packages
		// TODO: Truncate large packages?

		docPkg, err := buildDoc(importPath, pkg)
		if err != nil {
			// TODO: Surface this error somewhere
			log.Printf("Failed to build documentation for %s: %v", importPath, err)
			continue
		}
		if err := s.db.PutPackage(tx, platform, mod, docPkg, source); err != nil {
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
	if time.Since(timestamp) < s.cfg.MaxAge {
		return
	}
	log.Println("REFRESH", modulePath)
	if err := s.fetchModule(ctx, s.cfg.Platform, modulePath, internal.LatestVersion); err != nil {
		log.Printf("Error refreshing %s: %v", modulePath, err)
		return
	}
}
