// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

const testTimeout = 5 * time.Second

const (
	sampleModulePath    = "github.com/valid/module_name"
	sampleRepositoryURL = "https://github.com/valid/module_name"
	sampleVersionString = "v1.0.0"

	MITLicense = `Copyright 2019 Google Inc

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.`

	BSD0License = `Copyright 2019 Google Inc

Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.`

	UnknownLicense = `THIS IS A LICENSE THAT I JUST MADE UP. YOU CAN DO WHATEVER YOU WANT WITH THIS CODE, TRUST ME.`
)

var testModule = &Module{
	ModulePath: sampleModulePath,
	Version:    sampleVersionString,
	Files: map[string]string{
		"go.mod":      "module github.com/my/module\n\ngo 1.12",
		"LICENSE":     BSD0License,
		"README.md":   "README FILE FOR TESTING.",
		"bar/LICENSE": MITLicense,
		"bar/bar.go": `
						// package bar
						package bar

						// Bar returns the string "bar".
						func Bar() string {
							return "bar"
						}`,
		"foo/LICENSE.md": MITLicense,
		"foo/foo.go": `
						// package foo
						package foo

						import (
							"fmt"

							"github.com/my/module/bar"
						)

						// FooBar returns the string "foo bar".
						func FooBar() string {
							return fmt.Sprintf("foo %s", bar.Bar())
						}`,
	},
}

const uncachedModulePath = "example.com/uncached"

var uncachedModule = &Module{
	ModulePath: uncachedModulePath,
	Version:    sampleVersionString,
	NotCached:  true,
}

func TestGetLatestInfo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	testModules := []*Module{
		{
			ModulePath: sampleModulePath,
			Version:    "v1.1.0",
			Files:      map[string]string{"bar.go": "package bar\nconst Version = 1.1"},
		},
		{
			ModulePath: sampleModulePath,
			Version:    "v1.2.0",
			Files:      map[string]string{"bar.go": "package bar\nconst Version = 1.2"},
		},
	}
	client, teardownProxy := setupTestClient(t, testModules)
	defer teardownProxy()

	info, err := client.GetInfo(ctx, sampleModulePath, LatestVersion)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := info.Version, "v1.2.0"; got != want {
		t.Errorf("GetInfo(ctx, %q, %q): Version = %q, want %q", sampleModulePath, LatestVersion, got, want)
	}
}

func TestListVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	testModules := []*Module{
		{
			ModulePath: sampleModulePath,
			Version:    "v1.1.0",
			Files:      map[string]string{"bar.go": "package bar\nconst Version = 1.1"},
		},
		{
			ModulePath: sampleModulePath,
			Version:    "v1.2.0",
			Files:      map[string]string{"bar.go": "package bar\nconst Version = 1.2"},
		},
		{
			ModulePath: sampleModulePath + "/bar",
			Version:    "v1.3.0",
			Files:      map[string]string{"bar.go": "package bar\nconst Version = 1.3"},
		},
	}
	client, teardownProxy := setupTestClient(t, testModules)
	defer teardownProxy()

	want := []string{"v1.1.0", "v1.2.0"}
	got, err := client.ListVersions(ctx, sampleModulePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("got: %q, want: %q", got, want)
	}
}

func TestGetInfo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	client, teardownProxy := setupTestClient(t, []*Module{testModule, uncachedModule})
	defer teardownProxy()

	info, err := client.GetInfo(ctx, sampleModulePath, sampleVersionString)
	if err != nil {
		t.Fatal(err)
	}

	if info.Version != sampleVersionString {
		t.Errorf("VersionInfo.Version for GetInfo(ctx, %q, %q) = %q, want %q",
			sampleModulePath, sampleVersionString, info.Version, sampleVersionString)
	}
	expectedTime := time.Date(2019, 1, 30, 0, 0, 0, 0, time.UTC)
	if info.Time != expectedTime {
		t.Errorf("VersionInfo.Time for GetInfo(ctx, %q, %q) = %v, want %v", sampleModulePath, sampleVersionString, info.Time, expectedTime)
	}

	// GetInfoNoFetch returns "NotFetched" error on uncached module.
	_, err = client.GetInfoNoFetch(ctx, uncachedModulePath, sampleVersionString)
	if !errors.Is(err, ErrNotFetched) {
		t.Fatalf("got %v, want NotFetched", err)
	}
	// GetInfoNoFetch succeeds on cached module.
	_, err = client.GetInfoNoFetch(ctx, sampleModulePath, sampleVersionString)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetInfo_Errors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	proxyServer := NewServer(nil)
	proxyServer.AddRoute(
		fmt.Sprintf("/%s/@v/%s.info", "module.com/timeout", sampleVersionString),
		func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fetch timed out", http.StatusNotFound) })
	client, teardownProxy := newClientForServer(proxyServer)
	defer teardownProxy()

	for _, test := range []struct {
		modulePath string
		want       error
	}{
		{
			modulePath: sampleModulePath,
			want:       ErrNotFound,
		},
		{
			modulePath: "module.com/timeout",
			want:       ErrProxyTimedOut,
		},
	} {
		if _, err := client.GetInfo(ctx, test.modulePath, sampleVersionString); !errors.Is(err, test.want) {
			t.Errorf("got %v, want %v", err, test.want)
		}
	}
}

func TestGetMod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	client, teardownProxy := setupTestClient(t, []*Module{testModule})
	defer teardownProxy()

	bytes, err := client.GetMod(ctx, sampleModulePath, sampleVersionString)
	if err != nil {
		t.Fatal(err)
	}
	got := string(bytes)
	want := "module github.com/my/module\n\ngo 1.12"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGetZip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	client, teardownProxy := setupTestClient(t, []*Module{testModule})
	defer teardownProxy()

	zipReader, err := client.GetZip(ctx, sampleModulePath, sampleVersionString)
	if err != nil {
		t.Fatal(err)
	}

	wantFiles := []string{
		sampleModulePath + "@" + sampleVersionString + "/LICENSE",
		sampleModulePath + "@" + sampleVersionString + "/README.md",
		sampleModulePath + "@" + sampleVersionString + "/go.mod",
		sampleModulePath + "@" + sampleVersionString + "/foo/foo.go",
		sampleModulePath + "@" + sampleVersionString + "/foo/LICENSE.md",
		sampleModulePath + "@" + sampleVersionString + "/bar/bar.go",
		sampleModulePath + "@" + sampleVersionString + "/bar/LICENSE",
	}
	if len(zipReader.File) != len(wantFiles) {
		t.Errorf("GetZip(ctx, %q, %q) returned number of files: got %d, want %d",
			sampleModulePath, sampleVersionString, len(zipReader.File), len(wantFiles))
	}

	expectedFileSet := map[string]bool{}
	for _, ef := range wantFiles {
		expectedFileSet[ef] = true
	}
	for _, zipFile := range zipReader.File {
		if !expectedFileSet[zipFile.Name] {
			t.Errorf("GetZip(ctx, %q, %q) returned unexpected file: %q", sampleModulePath,
				sampleVersionString, zipFile.Name)
		}
		expectedFileSet[zipFile.Name] = false
	}
}

func TestGetZipNonExist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	client, teardownProxy := setupTestClient(t, nil)
	defer teardownProxy()

	if _, err := client.GetZip(ctx, sampleModulePath, sampleVersionString); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want %v", err, ErrNotFound)
	}
}

func TestGetZipSize(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		client, teardownProxy := setupTestClient(t, []*Module{testModule})
		defer teardownProxy()
		got, err := client.GetZipSize(context.Background(), sampleModulePath, sampleVersionString)
		if err != nil {
			t.Error(err)
		}
		const want = 3235
		if got != want {
			t.Errorf("got %d, want %d", got, want)
		}
	})
	t.Run("not found", func(t *testing.T) {
		client, teardownProxy := setupTestClient(t, nil)
		defer teardownProxy()
		if _, err := client.GetZipSize(context.Background(), sampleModulePath, sampleVersionString); !errors.Is(err, ErrNotFound) {
			t.Errorf("got %v, want %v", err, ErrNotFound)
		}
	})
}

func TestEncodedURL(t *testing.T) {
	c := &Client{URL: "u"}
	for _, test := range []struct {
		path, version, suffix string
		want                  string // empty => error
	}{
		{
			"mod.com", "v1.0.0", "info",
			"u/mod.com/@v/v1.0.0.info",
		},
		{
			"mod", "v1.0.0", "info",
			"", // bad module path
		},
		{
			"mod.com", "v1.0.0-rc1", "info",
			"u/mod.com/@v/v1.0.0-rc1.info",
		},
		{
			"mod.com/Foo", "v1.0.0-RC1", "info",
			"u/mod.com/!foo/@v/v1.0.0-!r!c1.info",
		},
		{
			"mod.com", ".", "info",
			"", // bad version
		},
		{
			"mod.com", "v1.0.0", "zip",
			"u/mod.com/@v/v1.0.0.zip",
		},
		{
			"mod", "v1.0.0", "zip",
			"", // bad module path
		},
		{
			"mod.com", "v1.0.0-rc1", "zip",
			"u/mod.com/@v/v1.0.0-rc1.zip",
		},
		{
			"mod.com/Foo", "v1.0.0-RC1", "zip",
			"u/mod.com/!foo/@v/v1.0.0-!r!c1.zip",
		},
		{
			"mod.com", ".", "zip",
			"", // bad version
		},
		{
			"mod.com", LatestVersion, "info",
			"u/mod.com/@latest",
		},
		{
			"mod.com", LatestVersion, "zip",
			"", // can't ask for latest zip
		},
		{
			"mod.com", "v1.0.0", "other",
			"", // only "info" or "zip"
		},
	} {
		got, err := c.escapedURL(test.path, test.version, test.suffix)
		if got != test.want || (err != nil) != (test.want == "") {
			t.Errorf("%s, %s, %s: got (%q, %v), want %q", test.path, test.version, test.suffix, got, err, test.want)
		}
	}
}
