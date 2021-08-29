// Package source fetches module source code from module proxies.
// It also provides links to online source code repositories.
package source

import (
	"bytes"
	"context"
	"go/build"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// Source represents a source of Go modules.
type Source interface {
	LatestVersion(ctx context.Context, modulePath string) (string, error)
	Versions(ctx context.Context, modulePath string) ([]string, error)
	Get(ctx context.Context, modulePath, version string) (*Module, error)
}

// Module represents a module.
type Module struct {
	Path     string     // module path
	Version  string     // module version
	Time     time.Time  // commit time
	Packages []*Package // packages
}

// Package represents a package.
type Package struct {
	Path  string  // package path
	Files []*File // source files
}

// File represents a source file.
type File struct {
	Name     string // file name
	Contents []byte // file contents
}

// BuildContext transforms the provided build context into one suitable
// for use with this package.
func (pkg *Package) BuildContext(ctx *build.Context) *build.Context {
	safeCopy := *ctx
	ctx = &safeCopy
	ctx.JoinPath = path.Join
	ctx.IsAbsPath = path.IsAbs
	ctx.SplitPathList = func(list string) []string { return strings.Split(list, ":") }
	ctx.IsDir = func(path string) bool { return path == "." }
	ctx.HasSubdir = func(root, dir string) (rel string, ok bool) { return "", false }
	ctx.ReadDir = pkg.readDir
	ctx.OpenFile = pkg.openFile
	return ctx
}

func (pkg *Package) readDir(name string) ([]os.FileInfo, error) {
	if name != "." {
		return nil, os.ErrNotExist
	}
	fis := make([]os.FileInfo, len(pkg.Files))
	for i, f := range pkg.Files {
		fis[i] = fileInfo{f}
	}
	return fis, nil
}

func (pkg *Package) openFile(path string) (io.ReadCloser, error) {
	name := strings.TrimPrefix(path, "./")
	for _, f := range pkg.Files {
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
