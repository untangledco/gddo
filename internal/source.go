package internal

import (
	"bytes"
	"errors"
	"go/build"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const LatestVersion = "latest"

var (
	// ErrNotFound indicates that the requested module was not found.
	ErrNotFound = errors.New("not found")

	// ErrInvalidPath indicates that the requested module path is invalid.
	ErrInvalidPath = errors.New("invalid path")

	// ErrInvalidVersion indicates that the requested version is invalid.
	ErrInvalidVersion = errors.New("invalid version")

	// ErrBadModule indicates a problem with a module.
	ErrBadModule = errors.New("bad module")
)

// Source represents a source of Go modules.
type Source interface {
	Module(modulePath, version string) (*Module, error)
	Files(module *Module) (fs.FS, error)
}

// Module contains module information.
type Module struct {
	ModulePath    string
	SeriesPath    string
	Version       string
	CommitTime    time.Time
	LatestVersion string
	Versions      []string
	Deprecated    string
}

// Package contains package information.
type Package struct {
	Module
	ImportPath string
	Imports    []string
	Name       string
	Synopsis   string
	Updated    time.Time
}

// Directory represents a package directory.
type Directory struct {
	Path  string
	Files []File
}

// File represents a source file.
type File struct {
	Name     string
	Contents []byte
}

// BuildContext transforms the provided build context into one suitable
// for use with this directory.
func (dir *Directory) BuildContext(ctx *build.Context) *build.Context {
	safeCopy := *ctx
	ctx = &safeCopy
	ctx.JoinPath = path.Join
	ctx.IsAbsPath = path.IsAbs
	ctx.SplitPathList = func(list string) []string { return strings.Split(list, ":") }
	ctx.IsDir = func(path string) bool { return path == "." }
	ctx.HasSubdir = func(root, dir string) (rel string, ok bool) { return "", false }
	ctx.ReadDir = dir.readDir
	ctx.OpenFile = dir.openFile
	return ctx
}

func (dir *Directory) readDir(name string) ([]os.FileInfo, error) {
	if name != "." {
		return nil, os.ErrNotExist
	}
	fis := make([]os.FileInfo, len(dir.Files))
	for i := range dir.Files {
		fis[i] = fileInfo{&dir.Files[i]}
	}
	return fis, nil
}

func (dir *Directory) openFile(path string) (io.ReadCloser, error) {
	name := strings.TrimPrefix(path, "./")
	for _, f := range dir.Files {
		if f.Name == name {
			return io.NopCloser(bytes.NewReader(f.Contents)), nil
		}
	}
	return nil, os.ErrNotExist
}

type fileInfo struct{ f *File }

func (fi fileInfo) Name() string       { return fi.f.Name }
func (fi fileInfo) Size() int64        { return int64(len(fi.f.Contents)) }
func (fi fileInfo) Mode() os.FileMode  { return 0 }
func (fi fileInfo) ModTime() time.Time { return time.Time{} }
func (fi fileInfo) IsDir() bool        { return false }
func (fi fileInfo) Sys() interface{}   { return nil }

// ParseDirectories parses package directories from the given filesystem.
func ParseDirectories(fsys fs.FS) ([]Directory, error) {
	dirsMap := map[string]*Directory{}
	fs.WalkDir(fsys, ".", func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			// Skip ignored directories
			if ignoredByGoTool(fpath) || isVendored(fpath) {
				return fs.SkipDir
			}
			// Add the directory to the map
			dirsMap[fpath] = &Directory{
				Path: fpath,
			}
			return nil
		}

		// Skip ignored files
		if ignoredByGoTool(fpath) || !strings.HasSuffix(fpath, ".go") {
			return nil
		}

		// Add file
		b, err := fs.ReadFile(fsys, fpath)
		if err != nil {
			return err
		}
		dir := dirsMap[path.Dir(fpath)]
		dir.Files = append(dir.Files, File{
			Name:     d.Name(),
			Contents: b,
		})

		return nil
	})

	// Sort directories by path
	var dirs []Directory
	for _, dir := range dirsMap {
		dirs = append(dirs, *dir)
	}
	sort.Sort(byPath(dirs))

	return dirs, nil
}

type byPath []Directory

func (s byPath) Len() int           { return len(s) }
func (s byPath) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byPath) Less(i, j int) bool { return s[i].Path < s[j].Path }

// ignoredByGoTool reports whether the given import path corresponds
// to a directory that would be ignored by the go tool.
//
// The logic of the go tool for ignoring directories is documented at
// https://golang.org/cmd/go/#hdr-Package_lists_and_patterns:
//
// 	Directory and file names that begin with "." or "_" are ignored
// 	by the go tool, as are directories named "testdata".
//
// However, even though `go list` and other commands that take package
// wildcards will ignore these, they can still be imported and used in
// working Go programs. We continue to ignore the "." and "testdata"
// cases, but we've seen valid Go packages with "_", so we accept those.
func ignoredByGoTool(importPath string) bool {
	for _, el := range strings.Split(importPath, "/") {
		if strings.HasPrefix(el, ".") && len(el) != 1 || el == "testdata" {
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
