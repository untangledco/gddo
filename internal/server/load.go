package server

import (
	"context"
	"errors"
	"go/doc"
	"go/token"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
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
	Doc           *doc.Package
	Project       *meta.Project
	Platform      string
	Dir           string
	Imported      []internal.Package
	SubPackages   []internal.Package
	Message       string
	platformParam bool
	allExamples   []*texample
	fset          *token.FileSet
}

// load loads a package.
func (s *Server) load(ctx context.Context, platform, importPath, version string, mode LoadMode) (*Package, error) {
	if s.db == nil {
		// Load the package directly from the source
		return s.loadPackageDirect(ctx, platform, importPath, version, mode)
	}
	return s.loadPackage(ctx, platform, importPath, version, mode)
}

func (s *Server) loadPackage(ctx context.Context, platform, importPath, version string, mode LoadMode) (*Package, error) {
	dpkg, ok, err := s.db.Package(ctx, platform, importPath, version)
	if err != nil {
		return nil, err
	}
	if !ok {
		err := s.fetch(ctx, platform, importPath, version)
		if err != nil {
			return nil, err
		}
		dpkg, ok, err = s.db.Package(ctx, platform, importPath, version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, internal.ErrNotFound
		}
	}

	var pkg Package
	pkg.Package = dpkg
	pkg.Platform = platform
	// Platform parameters are only needed when not on the default platform
	pkg.platformParam = pkg.Platform != s.cfg.Platform
	// Compute package directory (relative to module path)
	pkg.Dir = strings.TrimPrefix(pkg.ImportPath, pkg.ModulePath)
	pkg.Dir = strings.TrimPrefix(pkg.Dir, "/")

	if mode&NeedDocumentation != 0 {
		dir, err := s.db.Directory(ctx, platform, pkg.ImportPath, pkg.Version)
		if err != nil {
			return nil, err
		}
		pdoc, err := buildDoc(pkg.ImportPath, dir)
		if err != nil {
			return nil, err
		}
		pkg.Doc = pdoc
		pkg.fset = dir.FileSet()
	}

	if mode&NeedSubPackages != 0 {
		subpkgs, err := s.db.SubPackages(ctx, platform, pkg.ModulePath, pkg.Version, pkg.ImportPath)
		if err != nil {
			return nil, err
		}
		pkg.SubPackages = subpkgs
	}

	if mode&NeedImports != 0 {
		imports, err := s.db.Packages(ctx, platform, pkg.Imports)
		if err != nil {
			return nil, err
		}
		pkg.Imported = imports
	}

	if mode&NeedProject != 0 {
		project, ok, err := s.db.Project(ctx, pkg.SeriesPath)
		if err != nil {
			return nil, err
		}
		if ok {
			pkg.Project = &project
		}
	}

	return &pkg, nil
}

func (s *Server) loadPackageDirect(ctx context.Context, platform, importPath, version string, mode LoadMode) (*Package, error) {
	// Loop through potential module paths
	var source internal.Source
	var mod *internal.Module
	if stdlib.Contains(importPath) {
		var err error
		source, mod, err = s.sources.FindModule(stdlib.ModulePath, version)
		if err != nil {
			return nil, err
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
				return nil, err
			}
			break
		}
	}
	if mod == nil {
		return nil, internal.ErrNotFound
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
			return nil, err
		}
		dirs, err := parseDirs(platform, fsys)
		if err != nil {
			return nil, err
		}
		relPath := strings.TrimPrefix(pkg.ImportPath, pkg.ModulePath)
		relPath = strings.TrimPrefix(relPath, "/")
		relPath = path.Clean(relPath)
		dir := dirs[relPath]
		if dir == nil {
			return nil, internal.ErrNotFound
		}
		pdoc, err := buildDoc(pkg.ImportPath, dir)
		if err != nil {
			return nil, err
		}
		pkg.Doc = pdoc
		pkg.fset = dir.FileSet()

		isModule := mod.ModulePath == importPath

		if mode&NeedSubPackages != 0 {
			for dPath := range dirs {
				subPath := moduleImportPath(pkg.ModulePath, dPath)
				if (!isModule && !strings.HasPrefix(subPath, pkg.ImportPath+"/")) ||
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

	return &pkg, nil
}
