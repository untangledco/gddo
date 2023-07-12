// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package proxy provides support for fetching modules from a Go module proxy.
package proxy

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var (
	// ErrProxyTimedOut indicates that a request to the module proxy timed out.
	ErrProxyTimedOut = errors.New("proxy timed out")
)

// Source fetches Go modules from a module proxy.
type Source struct {
	// URL of the module proxy web server.
	URL string

	// Client used for HTTP requests.
	HTTPClient *http.Client
}

// Module fetches a module from the module proxy. If the module is in the
// standard library, it is fetched from the Go git repository instead.
func (s *Source) Module(modulePath, version string) (*internal.Module, error) {
	// Get version info
	info, err := s.getInfo(modulePath, version)
	if err != nil {
		return nil, err
	}
	latest, err := s.getInfo(modulePath, internal.LatestVersion)
	if err != nil {
		return nil, err
	}

	versions, err := s.listVersions(modulePath)
	if err != nil {
		return nil, err
	}

	// Get module file
	mod, err := s.getMod(modulePath, info.Version)
	if err != nil {
		return nil, err
	}
	// Get module path
	if path := modfile.ModulePath(mod); path != "" {
		modulePath = path
	}
	// Get deprecated
	var deprecated string
	latestMod, err := s.getMod(modulePath, latest.Version)
	if err != nil {
		return nil, err
	}
	if file, err := modfile.ParseLax("go.mod", latestMod, nil); err == nil {
		deprecated = file.Module.Deprecated
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	reference := info.Version
	if module.IsPseudoVersion(reference) {
		// The reference cannot be easily determined from the pseudo-version
		reference = ""
	}

	return &internal.Module{
		ModulePath:    modulePath,
		SeriesPath:    seriesPath,
		Version:       info.Version,
		Reference:     reference,
		CommitTime:    info.Time,
		LatestVersion: latest.Version,
		Versions:      versions,
		Deprecated:    deprecated,
	}, nil
}

// Files returns the module's files.
func (s *Source) Files(mod *internal.Module) (fs.FS, error) {
	// Get module zip
	zip, err := s.getZip(mod.ModulePath, mod.Version)
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("%s@%s", mod.ModulePath, mod.Version)
	fsys, err := fs.Sub(zip, prefix)
	if err != nil {
		return nil, err
	}
	return fsys, nil
}

// versionInfo contains metadata about a given version of a module.
type versionInfo struct {
	Version string
	Time    time.Time
}

// getInfo makes a request to $GOPROXY/<module>/@v/<requestedVersion>.info and
// transforms that data into a *versionInfo.
func (s *Source) getInfo(modulePath, requestedVersion string) (*versionInfo, error) {
	data, err := s.readBody(modulePath, requestedVersion, "info")
	if err != nil {
		return nil, err
	}
	var v versionInfo
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// getMod makes a request to $GOPROXY/<module>/@v/<resolvedVersion>.mod and returns the raw data.
func (s *Source) getMod(modulePath, resolvedVersion string) ([]byte, error) {
	return s.readBody(modulePath, resolvedVersion, "mod")
}

// getZip makes a request to $GOPROXY/<modulePath>/@v/<resolvedVersion>.zip and
// transforms that data into a *zip.Reader. <resolvedVersion> must have already
// been resolved by first making a request to
// $GOPROXY/<modulePath>/@v/<requestedVersion>.info to obtain the valid
// semantic version.
func (s *Source) getZip(modulePath, resolvedVersion string) (*zip.Reader, error) {
	bodyBytes, err := s.readBody(modulePath, resolvedVersion, "zip")
	if err != nil {
		return nil, err
	}
	zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("zip.NewReader: %v: %w", err, internal.ErrBadModule)
	}
	return zipReader, nil
}

func (s *Source) escapedURL(modulePath, requestedVersion, suffix string) (string, error) {
	if suffix != "info" && suffix != "mod" && suffix != "zip" {
		return "", errors.New(`suffix must be "info", "mod" or "zip"`)
	}
	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return "", fmt.Errorf("path: %v: %w", err, internal.ErrInvalidPath)
	}
	if requestedVersion == internal.LatestVersion {
		if suffix != "info" {
			return "", fmt.Errorf("cannot ask for latest with suffix %q", suffix)
		}
		return fmt.Sprintf("%s/%s/@latest", s.URL, escapedPath), nil
	}
	escapedVersion, err := module.EscapeVersion(requestedVersion)
	if err != nil {
		return "", fmt.Errorf("version: %v: %w", err, internal.ErrInvalidVersion)
	}
	return fmt.Sprintf("%s/%s/@v/%s.%s", s.URL, escapedPath, escapedVersion, suffix), nil
}

func (s *Source) readBody(modulePath, requestedVersion, suffix string) ([]byte, error) {
	u, err := s.escapedURL(modulePath, requestedVersion, suffix)
	if err != nil {
		return nil, err
	}
	var data []byte
	err = s.executeRequest(u, func(body io.Reader) error {
		var err error
		data, err = io.ReadAll(body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// listVersions makes a request to $GOPROXY/<path>/@v/list and returns the
// resulting version strings.
func (s *Source) listVersions(modulePath string) ([]string, error) {
	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, internal.ErrInvalidPath
	}
	u := fmt.Sprintf("%s/%s/@v/list", s.URL, escapedPath)
	var versions []string
	err = s.executeRequest(u, func(body io.Reader) error {
		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			versions = append(versions, scanner.Text())
		}
		return scanner.Err()
	})
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// executeRequest executes an HTTP GET request for u, then calls the bodyFunc
// on the response body, if no error occurred.
func (s *Source) executeRequest(u string, bodyFunc func(body io.Reader) error) (err error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	r, err := s.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if err := responseError(r); err != nil {
		return err
	}
	return bodyFunc(r.Body)
}

// responseError translates the response status code to an appropriate error.
func responseError(r *http.Response) error {
	switch {
	case 200 <= r.StatusCode && r.StatusCode < 300:
		return nil
	case r.StatusCode == http.StatusNotFound,
		r.StatusCode == http.StatusGone:
		// Treat both 404 Not Found and 410 Gone responses
		// from the proxy as a "not found" error category.
		// If the response body contains "fetch timed out", treat this
		// as a 504 response so that we retry fetching the module version again
		// later.
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		d := string(data)
		switch {
		case strings.Contains(d, "fetch timed out"):
			err = ErrProxyTimedOut
		default:
			err = internal.ErrNotFound
		}
		return fmt.Errorf("%q: %w", d, err)
	default:
		return fmt.Errorf("unexpected status %d %s", r.StatusCode, r.Status)
	}
}
