package server

import (
	"context"
	"errors"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

// A LoadMode configures the amount of detail returned when loading a package.
type LoadMode int

const (
	NeedSubPackages LoadMode = 1 << iota
	NeedImports
	NeedProject
)

// load loads a package.
func (s *Server) load(ctx context.Context, platform, importPath, version string, mode LoadMode) (*Package, error) {
	if s.db == nil {
		// Load the package directly from the source
		return s.loadPackageDirect(ctx, platform, importPath, version, mode)
	}
	return s.loadPackage(ctx, platform, importPath, version, mode)
}

func (s *Server) loadPackage(ctx context.Context, platform, importPath, version string, mode LoadMode) (*Package, error) {
	dpkg, err := s.db.Package(ctx, platform, importPath, version)
	if err != nil {
		return nil, err
	}
	if dpkg == nil {
		// Try fetching the package
		err := s.fetch(ctx, platform, importPath, version)
		if err != nil {
			return nil, err
		}
		dpkg, err = s.db.Package(ctx, platform, importPath, version)
		if err != nil {
			return nil, err
		}
		if dpkg == nil {
			return nil, internal.ErrNotFound
		}
	}

	pkg := s.newPackage(dpkg, platform)
	src, err := internal.FastDecodePackage(dpkg.Source)
	if err != nil {
		return nil, err
	}
	if err := pkg.BuildDoc(src); err != nil {
		return nil, err
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

	return pkg, nil
}

func (s *Server) loadPackageDirect(ctx context.Context, platform, importPath, version string, mode LoadMode) (*Package, error) {
	source, mod, err := s.findModule(importPath, version)
	if err != nil {
		return nil, err
	}
	fsys, err := source.Files(mod)
	if err != nil {
		return nil, err
	}
	pkgs, err := parsePackages(platform, mod.ModulePath, fsys)
	if err != nil {
		return nil, err
	}
	src := pkgs[importPath]
	if src == nil {
		return nil, internal.ErrNotFound
	}

	dpkg := &database.Package{
		Module:     *mod,
		ImportPath: importPath,
	}
	pkg := s.newPackage(dpkg, platform)
	if err := pkg.BuildDoc(src); err != nil {
		return nil, err
	}

	if mode&NeedSubPackages != 0 {
		isRoot := importPath == mod.ModulePath
		prefix := importPath + "/"
		for subPath := range pkgs {
			if subPath == importPath {
				continue
			}
			if !isRoot && !strings.HasPrefix(subPath, prefix) {
				continue
			}
			pkg.SubPackages = append(pkg.SubPackages, database.Package{
				Module:     *mod,
				ImportPath: subPath,
			})
		}
	}

	if mode&NeedImports != 0 {
		// Populate import paths only
		var imports []database.Package
		for _, importPath := range pkg.Imports {
			imports = append(imports, database.Package{
				ImportPath: importPath,
			})
		}
		pkg.Imported = imports
	}

	return pkg, nil
}

func (s *Server) findModule(importPath, version string) (internal.Source, *internal.Module, error) {
	// Loop through potential module paths
	var source internal.Source
	var mod *internal.Module
	if stdlib.Contains(importPath) {
		var err error
		source, mod, err = s.sources.FindModule(stdlib.ModulePath, version)
		if err != nil {
			return nil, nil, err
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
				return nil, nil, err
			}
			break
		}
	}
	if mod == nil {
		return nil, nil, internal.ErrNotFound
	}
	return source, mod, nil
}
