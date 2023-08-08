// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"bytes"
	"context"
	"errors"
	"go/build"
	"go/parser"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/godoc"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
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
	mod, source, err := s.db.Package(ctx, platform, importPath, version)
	if err != nil {
		return nil, err
	}
	if mod == nil {
		// Try fetching the package
		err := s.fetch(ctx, platform, importPath, version)
		if err != nil {
			return nil, err
		}
		mod, source, err = s.db.Package(ctx, platform, importPath, version)
		if err != nil {
			return nil, err
		}
		if mod == nil {
			return nil, internal.ErrNotFound
		}
	}

	var src *godoc.Package
	if len(source) > 0 {
		var err error
		src, err = godoc.DecodePackage(source)
		if err != nil {
			return nil, err
		}
	}

	pkg, err := NewPackage(mod, platform, importPath, src)
	if err != nil {
		return nil, err
	}

	if mode&NeedSubPackages != 0 {
		subpkgs, err := s.db.SubPackages(ctx, platform, mod.ModulePath, mod.Version, importPath)
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
		project, err := s.db.Project(ctx, mod.SeriesPath)
		if err != nil {
			return nil, err
		}
		pkg.project = project
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
	pkgs, err := loadPackages(platform, mod.ModulePath, fsys)
	if err != nil {
		return nil, err
	}
	src := pkgs[importPath]
	if src == nil {
		return nil, internal.ErrNotFound
	}

	pkg, err := NewPackage(mod, platform, importPath, src)
	if err != nil {
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

// loadPackages loads Go packages from the given filesystem.
func loadPackages(platform, modulePath string, fsys fs.FS) (map[string]*godoc.Package, error) {
	if !platforms.Valid(platform) {
		return nil, ErrInvalidPlatform
	}
	goos, goarch, found := strings.Cut(platform, "/")
	if !found {
		return nil, ErrInvalidPlatform
	}

	files := map[string][]byte{}

	// bctx is used to make decisions about which of the .go files are included
	// by build constraints.
	bctx := &build.Context{
		GOOS:        goos,
		GOARCH:      goarch,
		CgoEnabled:  true,
		Compiler:    build.Default.Compiler,
		ReleaseTags: build.Default.ReleaseTags,

		JoinPath: path.Join,
		OpenFile: func(name string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(files[name])), nil
		},

		// If left nil, the default implementations of these read from disk,
		// which we do not want. None of these functions should be used
		// inside this function; it would be an internal error if they are.
		// Set them to non-nil values to catch if that happens.
		SplitPathList: func(string) []string { panic("internal error: unexpected call to SplitPathList") },
		IsAbsPath:     func(string) bool { panic("internal error: unexpected call to IsAbsPath") },
		IsDir:         func(string) bool { panic("internal error: unexpected call to IsDir") },
		HasSubdir:     func(string, string) (string, bool) { panic("internal error: unexpected call to HasSubdir") },
		ReadDir:       func(string) ([]os.FileInfo, error) { panic("internal error: unexpected call to ReadDir") },
	}

	// Collect Go files
	err := fs.WalkDir(fsys, ".", func(pathname string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip ignored directories
			if ignoredByGoTool(d.Name()) || isVendor(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		// Skip ignored files
		if ignoredByGoTool(pathname) || !strings.HasSuffix(pathname, ".go") {
			return nil
		}

		contents, err := fs.ReadFile(fsys, pathname)
		if err != nil {
			return err
		}
		files[pathname] = contents

		match, err := bctx.MatchFile(path.Split(pathname))
		if err != nil {
			return err
		}
		if !match {
			delete(files, pathname)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Build package documentation
	pkgs := map[string]*godoc.Package{}
	for pathname, contents := range files {
		innerPath := path.Dir(pathname)
		importPath := path.Join(modulePath, innerPath)
		if modulePath == stdlib.ModulePath {
			importPath = innerPath
		}

		pkg := pkgs[importPath]
		if pkg == nil {
			pkg = godoc.NewPackage()
			pkgs[importPath] = pkg
		}

		filename := path.Base(pathname)
		file, err := parser.ParseFile(pkg.Fset, filename, contents, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		removeNodes := true
		// Don't strip the seemingly unexported functions from the builtin package;
		// they are actually Go builtins like make, new, etc.
		if modulePath == stdlib.ModulePath && innerPath == "builtin" {
			removeNodes = false
		}
		pkg.AddFile(file, removeNodes)
	}

	// Add directories to the map
	for importPath := range pkgs {
		dirPath := importPath
		for dirPath != "." && dirPath != modulePath {
			dirPath = path.Dir(dirPath)
			_, ok := pkgs[dirPath]
			if ok {
				break
			}
			pkgs[dirPath] = nil
		}
	}

	return pkgs, nil
}

// ignoredByGoTool reports whether the given file or directory would be
// ignored by the go tool.
//
// The logic of the go tool for ignoring directories is documented at
// https://golang.org/cmd/go/#hdr-Package_lists_and_patterns:
//
//	Directory and file names that begin with "." or "_" are ignored
//	by the go tool, as are directories named "testdata".
//
// However, even though `go list` and other commands that take package
// wildcards will ignore these, they can still be imported and used in
// working Go programs. We continue to ignore the "." and "testdata"
// cases, but we've seen valid Go packages with "_", so we accept those.
func ignoredByGoTool(pathname string) bool {
	return pathname != "." && strings.HasPrefix(pathname, ".") ||
		pathname == "testdata"
}

// isVendor reports whether the given directory is a vendor directory.
//
// The logic for what is considered a vendor directory is documented at
// https://golang.org/cmd/go/#hdr-Vendor_Directories.
func isVendor(dir string) bool {
	return dir == "vendor"
}
