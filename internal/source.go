package internal

import (
	"errors"
	"io/fs"
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
	Updated       time.Time // TODO: remove this
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
