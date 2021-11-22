package server

import (
	"context"
	"errors"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/doc"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
)

// A LoadMode configures the amount of detail returned when loading a package.
type LoadMode int

const (
	NeedDocumentation LoadMode = 1 << iota
	NeedSubPackages
	NeedImports
	NeedMeta
)

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
	dpkg, ok, err := s.db.GetPackage(ctx, platform, importPath, version)
	if err != nil {
		return Package{}, err
	}
	if !ok {
		err := s.fetch(ctx, platform, importPath, version)
		if err != nil {
			return Package{}, err
		}
		dpkg, ok, err = s.db.GetPackage(ctx, platform, importPath, version)
		if err != nil {
			return Package{}, err
		}
		if !ok {
			return Package{}, internal.ErrNotFound
		}
	}
	pkg.Package = dpkg

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

	if mode&NeedMeta != 0 {
		meta, ok, err := s.db.Meta(ctx, pkg.SeriesPath)
		if err != nil {
			return Package{}, err
		}
		if ok {
			pkg.Meta = &meta
		}
	}

	return pkg, nil
}

func (s *Server) loadPackageDirect(ctx context.Context, platform, importPath, version string, mode LoadMode) (Package, error) {
	// Loop through potential module paths
	var mod *internal.Module
	for modulePath := importPath; modulePath != "."; modulePath = path.Dir(modulePath) {
		var err error
		mod, err = s.source.Module(modulePath, version)
		if err != nil {
			if errors.Is(err, internal.ErrNotFound) {
				// Try parent path
				continue
			}
			return Package{}, err
		}
		break
	}
	if mod == nil {
		return Package{}, internal.ErrNotFound
	}

	var pkg Package
	pkg.Module = *mod
	pkg.ImportPath = importPath

	if mode&NeedDocumentation != 0 {
		fsys, err := s.source.Files(mod)
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
		bctx, err := platforms.Parse(platform)
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

		if mode&NeedSubPackages != 0 {
			for _, d := range dirs {
				subPath := path.Join(pkg.ModulePath, d.Path)
				if !strings.HasPrefix(subPath, pkg.ImportPath) || subPath == pkg.ImportPath {
					continue
				}
				pkg.SubPackages = append(pkg.SubPackages, internal.Package{
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
