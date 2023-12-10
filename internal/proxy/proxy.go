// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package proxy provides support for fetching modules from a Go module proxy.
package proxy

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
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
	"golang.org/x/net/context/ctxhttp"
)

var (
	// ErrProxyTimedOut indicates that a request to the module proxy timed out.
	ErrProxyTimedOut = errors.New("proxy timed out")
)

// Client fetches Go modules from a module proxy.
type Client struct {
	// URL of the module proxy web server.
	URL string

	// Client used for HTTP requests.
	HTTPClient *http.Client

	// MaxZipSize is the maximum zip file size allowed for reading.
	MaxZipSize int64
}

// Module fetches a module from the module proxy. If the module is in the
// standard library, it is fetched from the Go git repository instead.
func (c *Client) Module(modulePath, version string) (*internal.Module, error) {
	// Get version info
	info, err := c.getInfo(modulePath, version)
	if err != nil {
		return nil, err
	}
	latest, err := c.getInfo(modulePath, internal.LatestVersion)
	if err != nil {
		return nil, err
	}

	versions, err := c.listVersions(modulePath)
	if err != nil {
		return nil, err
	}

	// Get module file
	mod, err := c.getMod(modulePath, info.Version)
	if err != nil {
		return nil, err
	}
	// Get module path
	if path := modfile.ModulePath(mod); path != "" {
		modulePath = path
	}
	// Get deprecated
	var deprecated string
	latestMod, err := c.getMod(modulePath, latest.Version)
	if err != nil {
		return nil, err
	}
	if file, err := modfile.ParseLax("go.mod", latestMod, nil); err == nil {
		deprecated = file.Module.Deprecated
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	reference := info.Version
	if module.IsPseudoVersion(reference) {
		// Use the pseudo-version rev
		rev, err := module.PseudoVersionRev(reference)
		if err != nil {
			return nil, err
		}
		reference = rev
	}

	zipSize, err := c.ZipSize(context.TODO(), modulePath, info.Version)
	if err != nil {
		return nil, err
	}
	if zipSize > c.MaxZipSize {
		return nil, internal.ErrTooLarge
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
		ZipSize:       zipSize,
	}, nil
}

// Files returns the module's files.
func (c *Client) Files(mod *internal.Module) (fs.FS, error) {
	// Get module zip
	zip, err := c.getZip(mod.ModulePath, mod.Version)
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

// ZipSize gets the size in bytes of the zip from the proxy, without downloading it.
// The version must be resolved, as by a call to Client.Info.
func (c *Client) ZipSize(ctx context.Context, modulePath, resolvedVersion string) (int64, error) {
	url, err := c.escapedURL(modulePath, resolvedVersion, "zip")
	if err != nil {
		return 0, err
	}
	res, err := ctxhttp.Head(ctx, c.HTTPClient, url)
	if err != nil {
		return 0, fmt.Errorf("ctxhttp.Head(ctx, client, %q): %v", url, err)
	}
	defer res.Body.Close()
	if err := responseError(res); err != nil {
		return 0, err
	}
	if res.ContentLength < 0 {
		return 0, errors.New("unknown content length")
	}
	return res.ContentLength, nil
}

// versionInfo contains metadata about a given version of a module.
type versionInfo struct {
	Version string
	Time    time.Time
}

// getInfo makes a request to $GOPROXY/<module>/@v/<requestedVersion>.info and
// transforms that data into a *versionInfo.
func (c *Client) getInfo(modulePath, requestedVersion string) (*versionInfo, error) {
	data, err := c.readBody(modulePath, requestedVersion, "info")
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
func (c *Client) getMod(modulePath, resolvedVersion string) ([]byte, error) {
	return c.readBody(modulePath, resolvedVersion, "mod")
}

// getZip makes a request to $GOPROXY/<modulePath>/@v/<resolvedVersion>.zip and
// transforms that data into a *zip.Reader. <resolvedVersion> must have already
// been resolved by first making a request to
// $GOPROXY/<modulePath>/@v/<requestedVersion>.info to obtain the valid
// semantic version.
func (c *Client) getZip(modulePath, resolvedVersion string) (*zip.Reader, error) {
	bodyBytes, err := c.readBody(modulePath, resolvedVersion, "zip")
	if err != nil {
		return nil, err
	}
	zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("zip.NewReader: %v: %w", err, internal.ErrBadModule)
	}
	return zipReader, nil
}

func (c *Client) escapedURL(modulePath, requestedVersion, suffix string) (string, error) {
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
		return fmt.Sprintf("%s/%s/@latest", c.URL, escapedPath), nil
	}
	escapedVersion, err := module.EscapeVersion(requestedVersion)
	if err != nil {
		return "", fmt.Errorf("version: %v: %w", err, internal.ErrInvalidVersion)
	}
	return fmt.Sprintf("%s/%s/@v/%s.%s", c.URL, escapedPath, escapedVersion, suffix), nil
}

func (c *Client) readBody(modulePath, requestedVersion, suffix string) ([]byte, error) {
	u, err := c.escapedURL(modulePath, requestedVersion, suffix)
	if err != nil {
		return nil, err
	}
	var data []byte
	err = c.executeRequest(u, func(body io.Reader) error {
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
func (c *Client) listVersions(modulePath string) ([]string, error) {
	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, internal.ErrInvalidPath
	}
	u := fmt.Sprintf("%s/%s/@v/list", c.URL, escapedPath)
	var versions []string
	err = c.executeRequest(u, func(body io.Reader) error {
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
func (c *Client) executeRequest(u string, bodyFunc func(body io.Reader) error) (err error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	r, err := c.HTTPClient.Do(req)
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
