// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package proxy provides a client for interacting with a module proxy.
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
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/net/context/ctxhttp"
)

var (
	// ErrNotFound indicates that a requested entity was not found (HTTP 404).
	ErrNotFound = errors.New("not found")

	// ErrNotFetched means that the proxy returned "not found" with the
	// Disable-Module-Fetch header set. We don't know if the module really
	// doesn't exist, or the proxy just didn't fetch it.
	ErrNotFetched = errors.New("not fetched by proxy")

	// ErrInvalidArgument indicates that the input into the request is invalid in
	// some way (HTTP 400).
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrBadModule indicates a problem with a module.
	ErrBadModule = errors.New("bad module")

	// ErrProxyTimedOut indicates that a request timed out when fetching from the Module Mirror.
	ErrProxyTimedOut = errors.New("proxy timed out")
)

// A Client is used by the fetch service to communicate with a module
// proxy. It handles all methods defined by go help goproxy.
type Client struct {
	// URL of the module proxy web server.
	URL string

	// Client used for HTTP requests.
	HTTPClient http.Client
}

// VersionInfo contains metadata about a given version of a module.
type VersionInfo struct {
	Version string
	Time    time.Time
}

// Setting this header to true prevents the proxy from fetching uncached
// modules.
const disableFetchHeader = "Disable-Module-Fetch"

// GetInfo makes a request to $GOPROXY/<module>/@v/<requestedVersion>.info and
// transforms that data into a *VersionInfo.
func (c *Client) GetInfo(ctx context.Context, modulePath, requestedVersion string) (*VersionInfo, error) {
	return c.getInfo(ctx, modulePath, requestedVersion, false)
}

// GetInfoNoFetch behaves like GetInfo, except that it sets the
// Disable-Module-Fetch header so that the proxy does not fetch a module it
// doesn't already know about.
func (c *Client) GetInfoNoFetch(ctx context.Context, modulePath, requestedVersion string) (*VersionInfo, error) {
	return c.getInfo(ctx, modulePath, requestedVersion, true)
}

func (c *Client) getInfo(ctx context.Context, modulePath, requestedVersion string, disableFetch bool) (*VersionInfo, error) {
	data, err := c.readBody(ctx, modulePath, requestedVersion, "info", disableFetch)
	if err != nil {
		return nil, err
	}
	var v VersionInfo
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetMod makes a request to $GOPROXY/<module>/@v/<resolvedVersion>.mod and returns the raw data.
func (c *Client) GetMod(ctx context.Context, modulePath, resolvedVersion string) ([]byte, error) {
	return c.readBody(ctx, modulePath, resolvedVersion, "mod", false)
}

// GetZip makes a request to $GOPROXY/<modulePath>/@v/<resolvedVersion>.zip and
// transforms that data into a *zip.Reader. <resolvedVersion> must have already
// been resolved by first making a request to
// $GOPROXY/<modulePath>/@v/<requestedVersion>.info to obtain the valid
// semantic version.
func (c *Client) GetZip(ctx context.Context, modulePath, resolvedVersion string) (*zip.Reader, error) {
	bodyBytes, err := c.readBody(ctx, modulePath, resolvedVersion, "zip", false)
	if err != nil {
		return nil, err
	}
	zipReader, err := zip.NewReader(bytes.NewReader(bodyBytes), int64(len(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("zip.NewReader: %v: %w", err, ErrBadModule)
	}
	return zipReader, nil
}

// GetZipSize gets the size in bytes of the zip from the proxy without downloading it.
// The version must be resolved, as by a call to Client.GetInfo.
func (c *Client) GetZipSize(ctx context.Context, modulePath, resolvedVersion string) (int64, error) {
	url, err := c.escapedURL(modulePath, resolvedVersion, "zip")
	if err != nil {
		return 0, err
	}
	res, err := ctxhttp.Head(ctx, &c.HTTPClient, url)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if err := responseError(res, false); err != nil {
		return 0, err
	}
	if res.ContentLength < 0 {
		return 0, errors.New("unknown content length")
	}
	return res.ContentLength, nil
}

const LatestVersion = "latest"

func (c *Client) escapedURL(modulePath, requestedVersion, suffix string) (string, error) {
	if suffix != "info" && suffix != "mod" && suffix != "zip" {
		return "", errors.New(`suffix must be "info", "mod" or "zip"`)
	}
	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return "", fmt.Errorf("path: %v: %w", err, ErrInvalidArgument)
	}
	if requestedVersion == LatestVersion {
		if suffix != "info" {
			return "", fmt.Errorf("cannot ask for latest with suffix %q", suffix)
		}
		return fmt.Sprintf("%s/%s/@latest", c.URL, escapedPath), nil
	}
	escapedVersion, err := module.EscapeVersion(requestedVersion)
	if err != nil {
		return "", fmt.Errorf("version: %v: %w", err, ErrInvalidArgument)
	}
	return fmt.Sprintf("%s/%s/@v/%s.%s", c.URL, escapedPath, escapedVersion, suffix), nil
}

func (c *Client) readBody(ctx context.Context, modulePath, requestedVersion, suffix string, disableFetch bool) ([]byte, error) {
	u, err := c.escapedURL(modulePath, requestedVersion, suffix)
	if err != nil {
		return nil, err
	}
	var data []byte
	err = c.executeRequest(ctx, u, disableFetch, func(body io.Reader) error {
		var err error
		data, err = ioutil.ReadAll(body)
		return err
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ListVersions makes a request to $GOPROXY/<path>/@v/list and returns the
// resulting version strings.
func (c *Client) ListVersions(ctx context.Context, modulePath string) ([]string, error) {
	escapedPath, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, ErrInvalidArgument
	}
	u := fmt.Sprintf("%s/%s/@v/list", c.URL, escapedPath)
	var versions []string
	err = c.executeRequest(ctx, u, false, func(body io.Reader) error {
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
func (c *Client) executeRequest(ctx context.Context, u string, disableFetch bool, bodyFunc func(body io.Reader) error) (err error) {
	defer func() {
		if ctx.Err() != nil {
			err = fmt.Errorf("%v: %w", err, ErrProxyTimedOut)
		}
	}()

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	if disableFetch {
		req.Header.Set(disableFetchHeader, "true")
	}
	r, err := ctxhttp.Do(ctx, &c.HTTPClient, req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if err := responseError(r, disableFetch); err != nil {
		return err
	}
	return bodyFunc(r.Body)
}

// responseError translates the response status code to an appropriate error.
func responseError(r *http.Response, fetchDisabled bool) error {
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
		//
		// If the Disable-Module-Fetch header was set, use a different
		// error code so we can tell the difference.
		data, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return err
		}
		d := string(data)
		switch {
		case strings.Contains(d, "fetch timed out"):
			err = ErrProxyTimedOut
		case fetchDisabled:
			err = ErrNotFetched
		default:
			err = ErrNotFound
		}
		return fmt.Errorf("%q: %w", d, err)
	default:
		return fmt.Errorf("unexpected status %d %s", r.StatusCode, r.Status)
	}
}
