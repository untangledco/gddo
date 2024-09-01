// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/godoc"
	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"golang.org/x/mod/module"
)

// A LoadMode configures the amount of detail returned when loading a package.
type LoadMode int

const (
	NeedDirectories LoadMode = 1 << iota
	NeedImports
	NeedProject
)

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
	}
	if dpkg == nil {
		return nil, internal.ErrNotFound
	}

	src, err := godoc.DecodePackage(dpkg.Source)
	if err != nil {
		return nil, err
	}
	pkg, err := NewPackage(&dpkg.Module, platform, importPath, src)
	if err != nil {
		return nil, err
	}

	if mode&NeedDirectories != 0 {
		dirs, err := s.db.Directories(ctx, platform, dpkg.ModulePath, dpkg.Version, importPath)
		if err != nil {
			return nil, err
		}
		pkg.Directories = dirs
	}

	if mode&NeedImports != 0 {
		imports, err := s.db.Synopses(ctx, platform, pkg.Imports)
		if err != nil {
			return nil, err
		}
		pkg.Imported = imports
	}

	if mode&NeedProject != 0 {
		project, err := s.db.Project(ctx, dpkg.ModulePath)
		if err != nil {
			return nil, err
		}
		pkg.project = project
	}

	return pkg, nil
}

// loadResult is the result of attempting to load a package.
// Only one of Package or Error will be populated.
type loadResult struct {
	Package *godoc.Package
	Error   string
}

// loadPackages loads Go packages from the given filesystem.
func loadPackages(platform, modulePath string, fsys fs.FS) (map[string]loadResult, error) {
	if !validPlatform(platform) {
		return nil, ErrInvalidPlatform
	}
	goos, goarch, found := strings.Cut(platform, "/")
	if !found {
		return nil, ErrInvalidPlatform
	}

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
			return fsys.Open(name)
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

	// Collect Go file names
	pkgPathnames := map[string][]string{}
	incompleteDirs := map[string]error{}
	err := fs.WalkDir(fsys, ".", func(pathname string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip ignored directories
			if ignoredByGoTool(d.Name()) || d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		// Skip ignored files
		if ignoredByGoTool(pathname) || !strings.HasSuffix(pathname, ".go") {
			return nil
		}
		innerPath := path.Dir(pathname)
		if _, ok := incompleteDirs[innerPath]; ok {
			// Something went wrong while processing this directory, so skip
			return nil
		}
		// It's possible to have a Go package in a directory that does not result in a valid import path.
		// That package cannot be imported, but that may be fine if it's a main package, intended to be
		// built and run from that directory.
		// We're not set up to handle invalid import paths, so skip these packages.
		if err := module.CheckFilePath(pathname); err != nil {
			log.Printf("module %s: %v", modulePath, err)
			incompleteDirs[innerPath] = err
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > MaxFileSize {
			log.Printf("module %s: file %s size %d exceeds max limit %d",
				modulePath, pathname, info.Size(), MaxFileSize)
			err := fmt.Errorf("Unable to process %s: file size %d exceeds max limit %d",
				pathname, info.Size(), MaxFileSize)
			incompleteDirs[innerPath] = err
			return nil
		}
		match, err := bctx.MatchFile(path.Split(pathname))
		if err != nil {
			return err
		}
		if !match {
			return nil
		}
		pkgPathnames[innerPath] = append(pkgPathnames[innerPath], pathname)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Build package documentation
	pkgPaths := []string{}
	results := map[string]loadResult{}
	for innerPath, pathnames := range pkgPathnames {
		importPath := path.Join(modulePath, innerPath)
		if modulePath == proxy.StdlibModulePath {
			importPath = innerPath
		}

		isBuiltin := false
		if modulePath == proxy.StdlibModulePath && innerPath == "builtin" {
			isBuiltin = true
		}
		pkg, err := godoc.ParseFiles(fsys, pathnames, isBuiltin)
		if err != nil {
			pkgPaths = append(pkgPaths, importPath)
			results[importPath] = loadResult{
				Error: err.Error(),
			}
			continue
		}
		pkgPaths = append(pkgPaths, importPath)
		results[importPath] = loadResult{
			Package: pkg,
		}
	}

	// Add incomplete directories to the map
	for innerPath, err := range incompleteDirs {
		importPath := path.Join(modulePath, innerPath)
		if modulePath == proxy.StdlibModulePath {
			importPath = innerPath
		}
		results[importPath] = loadResult{
			Error: err.Error(),
		}
	}

	// Add directories to the map
	rootPath := modulePath
	if modulePath == proxy.StdlibModulePath {
		rootPath = "."
	}
	if _, ok := results[modulePath]; !ok {
		results[modulePath] = loadResult{}
	}
	// Loop through packages, adding parent directories as necessary
	for _, importPath := range pkgPaths {
		if importPath == rootPath {
			continue
		}
		dirPath := path.Dir(importPath)
		for dirPath != rootPath {
			_, ok := results[dirPath]
			if ok {
				break
			}
			results[dirPath] = loadResult{}
			dirPath = path.Dir(dirPath)
		}
	}

	return results, nil
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
