package server

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/database"
	"git.sr.ht/~sircmpwn/gddo/internal/meta"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
)

// Package contains package information and documentation for use in templates.
type Package struct {
	database.Package
	Doc           *doc.Package
	Project       *meta.Project
	Platform      string
	Dir           string
	Imported      []database.Package
	SubPackages   []database.Package
	Message       string
	platformParam bool
	examples      []*Example
	fset          *token.FileSet
}

// newPackage returns a new package for use in templates.
func (s *Server) newPackage(pkg *database.Package, platform string) *Package {
	// Compute package directory (relative to module path)
	dir := strings.TrimPrefix(pkg.ImportPath, pkg.ModulePath)
	dir = strings.TrimPrefix(dir, "/")

	return &Package{
		Package:  *pkg,
		Platform: platform,
		Dir:      dir,
		// Platform parameters are only needed when not on the default platform
		platformParam: platform != s.cfg.Platform,
	}
}

// BuildDoc builds package documentation using the given source files.
func (p *Package) BuildDoc(src *internal.Package) error {
	docPkg, err := buildDoc(p.ImportPath, src)
	if err != nil {
		return err
	}
	p.Doc = docPkg
	p.fset = src.FileSet()
	return nil
}

// buildDoc builds documentation for the given package.
func buildDoc(importPath string, src *internal.Package) (*doc.Package, error) {
	var files []*ast.File
	for _, f := range src.Files {
		files = append(files, f.AST)
	}
	mode := doc.Mode(0)
	if importPath == "builtin" {
		mode |= doc.AllDecls
	}
	docPkg, err := doc.NewFromFiles(src.FileSet(), files, importPath, mode)
	if err != nil {
		return nil, err
	}
	if importPath == "builtin" {
		removeAssociations(docPkg)
	}
	return docPkg, nil
}

type byFuncName []*doc.Func

func (s byFuncName) Len() int           { return len(s) }
func (s byFuncName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byFuncName) Less(i, j int) bool { return s[i].Name < s[j].Name }

func removeAssociations(pkg *doc.Package) {
	for _, t := range pkg.Types {
		pkg.Funcs = append(pkg.Funcs, t.Funcs...)
		t.Funcs = nil
	}
	sort.Sort(byFuncName(pkg.Funcs))
}

func moduleImportPath(modulePath, dir string) string {
	if modulePath == stdlib.ModulePath && dir != "." {
		return dir
	}
	return path.Join(modulePath, dir)
}

// parsePackages parses package source files from the given filesystem.
func parsePackages(platform string, modulePath string, fsys fs.FS) (map[string]*internal.Package, error) {
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

	pkgs := map[string]*internal.Package{}
	err := fs.WalkDir(fsys, ".", func(pathname string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			importPath := moduleImportPath(modulePath, pathname)
			// Skip ignored directories
			if ignoredByGoTool(importPath) || isVendored(importPath) {
				return fs.SkipDir
			}
			// Add the package to the map
			pkgs[importPath] = internal.NewPackage()
			return nil
		}
		if ignoredByGoTool(pathname) {
			return nil
		}
		if !strings.HasSuffix(pathname, ".go") {
			return nil
		}

		contents, err := fs.ReadFile(fsys, pathname)
		if err != nil {
			return err
		}
		files[pathname] = contents

		match, err := bctx.MatchFile(".", pathname)
		if err != nil {
			return err
		}
		if !match {
			delete(files, pathname)
			return nil
		}

		importPath := moduleImportPath(modulePath, path.Dir(pathname))
		pkg := pkgs[importPath]
		ast, err := parser.ParseFile(pkg.FileSet(), d.Name(), contents, parser.ParseComments)
		if err != nil {
			return err
		}
		pkg.Files = append(pkg.Files, &internal.File{
			Name: d.Name(),
			AST:  ast,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pkgs, nil
}

// ignoredByGoTool reports whether the given import path corresponds
// to a directory that would be ignored by the go tool.
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
func ignoredByGoTool(importPath string) bool {
	for _, el := range strings.Split(importPath, "/") {
		if strings.HasPrefix(el, ".") || el == "testdata" {
			return true
		}
	}
	return false
}

// isVendored reports whether the given import path corresponds
// to a Go package that is inside a vendor directory.
//
// The logic for what is considered a vendor directory is documented at
// https://golang.org/cmd/go/#hdr-Vendor_Directories.
func isVendored(importPath string) bool {
	return strings.HasPrefix(importPath, "vendor/") ||
		strings.Contains(importPath, "/vendor/")
}
