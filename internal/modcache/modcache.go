package modcache

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"sort"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Source fetches Go modules from a local Go module cache.
type Source struct {
	FS fs.FS
}

// Module fetches a module from the module cache. If the module is in the
// standard library, it is fetched from the local Go tree instead.
func (s *Source) Module(modulePath, version string) (*internal.Module, error) {
	if stdlib.Contains(modulePath) {
		if version != internal.LatestVersion {
			// Only latest version supported
			return nil, internal.ErrNotFound
		}
		goVersion := stdlib.VersionForTag(runtime.Version())
		return &internal.Module{
			ModulePath:    modulePath,
			SeriesPath:    modulePath,
			Version:       goVersion,
			LatestVersion: goVersion,
			Versions:      []string{goVersion},
		}, nil
	}

	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, internal.ErrInvalidPath
	}

	fsys, err := fs.Sub(s.FS, path.Join("cache/download", escapedPath))
	if err != nil {
		return nil, err
	}

	versions, err := s.listVersions(fsys)
	if err != nil {
		return nil, err
	}

	// Use last version as the latest version
	// TODO: See if this is always correct
	latestVersion := ""
	if len(versions) > 0 {
		latestVersion = versions[len(versions)-1]
	}
	if version == internal.LatestVersion {
		version = latestVersion
	}

	sort.Slice(versions, func(i, j int) bool {
		return semver.Compare(versions[i], versions[j]) > 0
	})

	// Get version info
	info, err := s.getInfo(fsys, version)
	if err != nil {
		return nil, err
	}

	// Get module file
	mod, err := fs.ReadFile(fsys, path.Join("@v", version+".mod"))
	if err != nil {
		return nil, err
	}
	// Get module path
	if path := modfile.ModulePath(mod); path != "" {
		modulePath = path
	}
	// Get deprecated
	var deprecated string
	latestMod, err := fs.ReadFile(fsys, path.Join("@v", latestVersion+".mod"))
	if err != nil {
		return nil, err
	}
	if file, err := modfile.ParseLax("go.mod", latestMod, nil); err == nil {
		deprecated = file.Module.Deprecated
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	return &internal.Module{
		ModulePath:    modulePath,
		SeriesPath:    seriesPath,
		Version:       info.Version,
		CommitTime:    info.Time,
		LatestVersion: latestVersion,
		Versions:      versions,
		Deprecated:    deprecated,
	}, nil
}

// Files returns the module's files.
func (s *Source) Files(mod *internal.Module) (fs.FS, error) {
	if stdlib.Contains(mod.ModulePath) {
		return os.DirFS(path.Join(runtime.GOROOT(), "src", mod.ModulePath)), nil
	}

	escapedPath, err := module.EscapePath(mod.ModulePath)
	if err != nil {
		return nil, internal.ErrInvalidPath
	}
	return fs.Sub(s.FS, escapedPath+"@"+mod.Version)
}

// versionInfo contains metadata about a given version of a module.
type versionInfo struct {
	Version string
	Time    time.Time
}

func (s *Source) getInfo(fsys fs.FS, requestedVersion string) (*versionInfo, error) {
	data, err := fs.ReadFile(fsys, "@v/"+requestedVersion+".info")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, internal.ErrNotFound
		}
		return nil, err
	}
	var v versionInfo
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Source) listVersions(fsys fs.FS) ([]string, error) {
	data, err := fs.ReadFile(fsys, "@v/list")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, internal.ErrNotFound
		}
		return nil, err
	}
	var versions []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		versions = append(versions, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}
