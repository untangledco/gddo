package internal

import (
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
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

// Module contains module information.
type Module struct {
	ModulePath    string
	SeriesPath    string
	Version       string
	Reference     string
	CommitTime    time.Time
	LatestVersion string
	Versions      []string
	Deprecated    string
	Updated       time.Time
}

// Source represents a source of Go modules.
type Source interface {
	Module(modulePath, version string) (*Module, error)
	Files(module *Module) (fs.FS, error)
}

// SourceList fetches modules by trying a list of module sources.
type SourceList []Source

// FindModule finds the given module, returning the module and the module source
// which resolved it.
func (list SourceList) FindModule(modulePath, version string) (Source, *Module, error) {
	for _, source := range list {
		mod, err := source.Module(modulePath, version)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				// Try other sources
				continue
			}
			return nil, nil, err
		}
		return source, mod, nil
	}
	// Not found in any of the sources
	return nil, nil, ErrNotFound
}

// DirectorySource returns a module source which fetches a module from the given
// directory. The directory must contain a valid go.mod file. If no go.mod file
// is found, the returned Source will be nil.
func DirectorySource(dir string) (*ModuleSource, error) {
	fsys := os.DirFS(dir)

	// Parse go.mod
	mod, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// No go.mod file found
			return nil, nil
		}
		return nil, err
	}
	file, err := modfile.ParseLax("go.mod", mod, nil)
	if err != nil {
		return nil, err
	}
	if file.Module == nil {
		return nil, errors.New("go.mod missing module directive")
	}

	modulePath := file.Module.Mod.Path
	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	return &ModuleSource{
		Mod: &Module{
			ModulePath: modulePath,
			SeriesPath: seriesPath,
			Deprecated: file.Module.Deprecated,
		},
		FS: fsys,
	}, nil
}

// ModuleSource is a Source which serves a specific module only.
type ModuleSource struct {
	Mod *Module
	FS  fs.FS
}

func (s *ModuleSource) Module(modulePath, version string) (*Module, error) {
	if modulePath != s.Mod.ModulePath {
		return nil, ErrNotFound
	}
	return s.Mod, nil
}

func (s *ModuleSource) Files(mod *Module) (fs.FS, error) {
	return s.FS, nil
}
