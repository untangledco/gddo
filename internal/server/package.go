package server

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/platforms"
)

// parseDirs parses package directories from the given filesystem.
func parseDirs(platform string, fsys fs.FS) (map[string]*internal.Directory, error) {
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

	dirs := map[string]*internal.Directory{}
	err := fs.WalkDir(fsys, ".", func(pathname string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip ignored directories
			if ignoredByGoTool(pathname) || isVendored(pathname) {
				return fs.SkipDir
			}
			// Add the directory to the map
			dirs[pathname] = internal.NewDirectory()
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

		dir := dirs[path.Dir(pathname)]
		ast, err := parser.ParseFile(dir.FileSet(), d.Name(), contents, parser.ParseComments)
		if err != nil {
			return err
		}
		dir.Files = append(dir.Files, &internal.File{
			Name: d.Name(),
			AST:  ast,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}
	return dirs, nil
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
		if el != "." && strings.HasPrefix(el, ".") || el == "testdata" {
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

// buildDoc computes documentation for the given package directory.
func buildDoc(importPath string, dir *internal.Directory) (*doc.Package, error) {
	var files []*ast.File
	for _, f := range dir.Files {
		files = append(files, f.AST)
	}

	mode := doc.Mode(0)
	if importPath == "builtin" {
		mode |= doc.AllDecls
	}

	pkg, err := doc.NewFromFiles(dir.FileSet(), files, importPath, mode)
	if err != nil {
		return nil, err
	}

	if importPath == "builtin" {
		removeAssociations(pkg)
	}

	return pkg, nil
}

type byFuncName []*doc.Func

func (s byFuncName) Len() int           { return len(s) }
func (s byFuncName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byFuncName) Less(i, j int) bool { return s[i].Name < s[j].Name }

func removeAssociations(dpkg *doc.Package) {
	for _, t := range dpkg.Types {
		dpkg.Funcs = append(dpkg.Funcs, t.Funcs...)
		t.Funcs = nil
	}
	sort.Sort(byFuncName(dpkg.Funcs))
}
