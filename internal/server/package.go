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

// Package is a [doc.Package] with additional information for use in templates.
type Package struct {
	*internal.Module
	*doc.Package

	FileSet     *token.FileSet
	Synopsis    string
	Platform    string
	SubPackages []database.Package
	Imported    []database.Package
	Project     *meta.Project
	Message     string

	dir         string
	examples    []*Example
	examplesMap map[any][]*Example
}

// newPackage returns a new package for use in templates.
func (s *Server) newPackage(mod *internal.Module, platform, importPath string, src *internal.Package) (*Package, error) {
	// Build documentation
	docPkg, err := buildDoc(importPath, src)
	if err != nil {
		return nil, err
	}

	// Compute package directory (relative to module path)
	dir := strings.TrimPrefix(importPath, mod.ModulePath)
	dir = strings.TrimPrefix(dir, "/")

	pkg := &Package{
		Module:   mod,
		Package:  docPkg,
		FileSet:  src.FileSet(),
		Synopsis: docPkg.Synopsis(docPkg.Doc),
		Platform: platform,
		dir:      dir,
	}
	pkg.collectExamples()
	return pkg, nil
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
	pkg, err := doc.NewFromFiles(src.FileSet(), files, importPath, mode)
	if err != nil {
		return nil, err
	}
	if importPath == "builtin" {
		// Remove type associations
		for _, t := range pkg.Types {
			pkg.Funcs = append(pkg.Funcs, t.Funcs...)
			t.Funcs = nil
		}
		sort.Slice(pkg.Funcs, func(i, j int) bool {
			return pkg.Funcs[i].Name < pkg.Funcs[j].Name
		})
	}
	return pkg, nil
}

func moduleImportPath(modulePath, dir string) string {
	if modulePath == stdlib.ModulePath && dir != "." {
		return dir
	}
	return path.Join(modulePath, dir)
}

// Title returns a title for the package.
func (p *Package) Title() string {
	if p.ImportPath == stdlib.ModulePath {
		return "Standard library"
	}
	if p.Name != "" && p.Name != "main" {
		return p.Name
	}
	return path.Base(p.ImportPath)
}

// IsCommand reports whether p is a command package.
func (p *Package) IsCommand() bool {
	return p.Name == "main"
}

// Cgo reports whether the package uses Cgo.
func (p *Package) Cgo() bool {
	for i := range p.Imports {
		if p.Imports[i] == "C" {
			return true
		}
	}
	return false
}

// DirURL returns the URL for the package directory.
func (p *Package) DirURL() string {
	return p.Project.Dir(p.Reference, p.dir)
}

// FileURL returns the URL for the given file.
func (p *Package) FileURL(file string) string {
	return p.Project.File(p.Reference, p.dir, file)
}

// Example is a [doc.Example] with additional information for use in templates.
type Example struct {
	*doc.Example
	ID     string
	Symbol string
	Suffix string
	pkg    *Package
}

// collectExamples extracts examples into the internal examples representation.
func (p *Package) collectExamples() {
	p.examplesMap = make(map[any][]*Example)
	p.addExamples(p, "", p.Examples)
	for _, f := range p.Funcs {
		p.addExamples(f, f.Name, f.Examples)
	}
	for _, t := range p.Types {
		p.addExamples(t, t.Name, t.Examples)
		for _, f := range t.Funcs {
			p.addExamples(f, f.Name, f.Examples)
		}
		for _, m := range t.Methods {
			if len(m.Examples) > 0 {
				p.addExamples(m, t.Name+"."+m.Name, m.Examples)
			}
		}
	}
	sort.SliceStable(p.examples, func(i, j int) bool {
		return p.examples[i].Symbol < p.examples[j].Symbol
	})
}

func (p *Package) addExamples(obj any, symbol string, examples []*doc.Example) {
	for _, example := range examples {
		suffix := strings.Title(example.Suffix)
		ex := &Example{
			Example: example,
			ID:      exampleID(symbol, suffix),
			Symbol:  symbol,
			Suffix:  suffix,
			pkg:     p,
		}

		p.examples = append(p.examples, ex)
		p.examplesMap[obj] = append(p.examplesMap[obj], ex)
	}
}

func exampleID(symbol, suffix string) string {
	switch {
	case symbol == "" && suffix == "":
		return "example-package"
	case symbol == "" && suffix != "":
		return "example-package-" + suffix
	case symbol != "" && suffix == "":
		return "example-" + symbol
	case symbol != "" && suffix != "":
		return "example-" + symbol + "-" + suffix
	default:
		panic("unreachable")
	}
}

// AllExamples returns a list of all examples.
func (p *Package) AllExamples() []*Example {
	return p.examples
}

// PackageExamples returns a list of examples associated with the package.
func (p *Package) PackageExamples() []*Example {
	return p.ObjExamples(p)
}

// ObjExamples returns a list of examples for the given object.
func (p *Package) ObjExamples(obj any) []*Example {
	return p.examplesMap[obj]
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

		match, err := bctx.MatchFile(path.Split(pathname))
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
