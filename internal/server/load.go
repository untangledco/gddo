package server

import (
	"context"
	"errors"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/meta"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

// A LoadMode configures the amount of detail returned when loading a package.
type LoadMode int

const (
	NeedDocumentation LoadMode = 1 << iota
	NeedSubPackages
	NeedImports
	NeedProject
)

// Package contains package information and documentation for use in templates.
type Package struct {
	internal.Package
	doc.Documentation
	Project       *meta.Project
	Platform      string
	Dir           string
	Imported      []internal.Package
	SubPackages   []internal.Package
	Message       string
	platformParam bool
	allExamples   []*texample
}

// load loads a package.
func (s *Server) load(ctx context.Context, platform, importPath, version string, mode LoadMode) (Package, error) {
	if s.db == nil {
		// Load the package directly from the source
		return s.loadPackageDirect(ctx, platform, importPath, version, mode)
	}

	type result struct {
		pkg Package
		err error
	}

	ch := make(chan result, 1)
	go func() {
		ctx := context.Background()
		pkg, err := s.loadPackage(ctx, platform, importPath, version, mode)
		ch <- result{pkg, err}
	}()

	ctx, cancel := context.WithTimeout(ctx, s.cfg.FetchTimeout)
	defer cancel()

	select {
	case r := <-ch:
		return r.pkg, r.err
	case <-ctx.Done():
		return Package{}, ErrFetching
	}
}

func (s *Server) loadPackage(ctx context.Context, platform, importPath, version string, mode LoadMode) (Package, error) {
	var pkg Package
	dpkg, ok, err := s.db.Package(ctx, platform, importPath, version)
	if err != nil {
		return Package{}, err
	}
	if !ok {
		err := s.fetch(ctx, platform, importPath, version)
		if err != nil {
			return Package{}, err
		}
		dpkg, ok, err = s.db.Package(ctx, platform, importPath, version)
		if err != nil {
			return Package{}, err
		}
		if !ok {
			return Package{}, internal.ErrNotFound
		}
	}
	pkg.Package = dpkg
	pkg.Platform = platform
	// Platform parameters are only needed when not on the default platform
	pkg.platformParam = pkg.Platform != s.cfg.Platform
	// Compute package directory (relative to module path)
	pkg.Dir = strings.TrimPrefix(pkg.ImportPath, pkg.ModulePath)
	pkg.Dir = strings.TrimPrefix(pkg.Dir, "/")

	if mode&NeedDocumentation != 0 {
		doc, err := s.db.Documentation(ctx, platform, pkg.ImportPath, pkg.Version)
		if err != nil {
			return Package{}, err
		}
		pkg.Documentation = doc
	}

	if mode&NeedSubPackages != 0 {
		subpkgs, err := s.db.SubPackages(ctx, platform, pkg.ModulePath, pkg.Version, pkg.ImportPath)
		if err != nil {
			return Package{}, err
		}
		pkg.SubPackages = subpkgs
	}

	if mode&NeedImports != 0 {
		imports, err := s.db.Packages(ctx, platform, pkg.Imports)
		if err != nil {
			return Package{}, err
		}
		pkg.Imported = imports
	}

	if mode&NeedProject != 0 {
		project, ok, err := s.db.Project(ctx, pkg.SeriesPath)
		if err != nil {
			return Package{}, err
		}
		if ok {
			pkg.Project = &project
		}
	}

	return pkg, nil
}

func (s *Server) loadPackageDirect(ctx context.Context, platform, importPath, version string, mode LoadMode) (Package, error) {
	// Loop through potential module paths
	var source internal.Source
	var mod *internal.Module
	if stdlib.Contains(importPath) {
		var err error
		source, mod, err = s.sources.FindModule(stdlib.ModulePath, version)
		if err != nil {
			return Package{}, err
		}
	} else {
		for modulePath := importPath; modulePath != "."; modulePath = path.Dir(modulePath) {
			var err error
			source, mod, err = s.sources.FindModule(modulePath, version)
			if err != nil {
				if errors.Is(err, internal.ErrNotFound) {
					// Try parent path
					continue
				}
				return Package{}, err
			}
			break
		}
	}
	if mod == nil {
		return Package{}, internal.ErrNotFound
	}

	var pkg Package
	pkg.Module = *mod
	pkg.ImportPath = importPath
	pkg.Platform = platform
	// Platform parameters are only needed when not on the default platform
	pkg.platformParam = pkg.Platform != s.cfg.Platform
	// Compute package directory (relative to module path)
	pkg.Dir = strings.TrimPrefix(pkg.ImportPath, pkg.ModulePath)
	pkg.Dir = strings.TrimPrefix(pkg.Dir, "/")

	if mode&NeedDocumentation != 0 {
		fsys, err := source.Files(mod)
		if err != nil {
			return Package{}, err
		}
		dirs, err := internal.ParseDirectories(fsys)
		if err != nil {
			return Package{}, err
		}
		var dir *internal.Directory
		relPath := strings.TrimPrefix(pkg.ImportPath, pkg.ModulePath)
		relPath = strings.TrimPrefix(relPath, "/")
		relPath = path.Clean(relPath)
		for _, d := range dirs {
			if d.Path == relPath {
				dir = &d
				break
			}
		}
		if dir == nil {
			return Package{}, internal.ErrNotFound
		}
		bctx, err := buildContext(platform)
		if err != nil {
			return Package{}, err
		}
		pdoc, err := doc.New(mod.ModulePath, dir, dir.BuildContext(bctx))
		if err != nil {
			return Package{}, err
		}
		pkg.Documentation = pdoc.Documentation
		pkg.Name = pdoc.Name
		pkg.Synopsis = pdoc.Synopsis
		pkg.Imports = pdoc.Imports

		isModule := mod.ModulePath == importPath

		if mode&NeedSubPackages != 0 {
			for _, d := range dirs {
				subPath := moduleImportPath(pkg.ModulePath, d.Path)
				if (!isModule && !strings.HasPrefix(subPath, pkg.ImportPath)) ||
					subPath == pkg.ImportPath {
					continue
				}
				pkg.SubPackages = append(pkg.SubPackages, internal.Package{
					Module:     *mod,
					ImportPath: subPath,
				})
			}
		}
	}

	if mode&NeedImports != 0 {
		// Populate import paths only
		var imports []internal.Package
		for _, importPath := range pkg.Imports {
			imports = append(imports, internal.Package{
				ImportPath: importPath,
			})
		}
		pkg.Imported = imports
	}

	return pkg, nil
}
