// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stdlib

import (
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/mod/module"
)

var (
	testVersion    = "v0.0.0-20190904010203-89fb59e2e920"
	testCommitTime = time.Date(2019, 9, 4, 1, 2, 3, 0, time.UTC)
	testVersions   = []string{
		"v1.2.1",
		"v1.3.2",
		"v1.4.2",
		"v1.4.3",
		"v1.6.0",
		"v1.6.3",
		"v1.6.0-beta.1",
		"v1.8.0",
		"v1.8.0-rc.2",
		"v1.9.0-rc.1",
		"v1.11.0",
		"v1.12.0",
		"v1.12.1",
		"v1.12.5",
		"v1.12.9",
		"v1.13.0",
		"v1.13.0-beta.1",
		"v1.14.6",
		"master",
	}
)

// testDataPath returns a path corresponding to a path relative to the calling
// test file. For convenience, rel is assumed to be "/"-delimited.
//
// It panics on failure.
func testDataPath(rel string) (s string) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("unable to determine relative path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), filepath.FromSlash(rel)))
}

// getTestGoRepo gets a Go repo for testing.
func getTestGoRepo(version string) (*git.Repository, error) {
	if strings.HasPrefix(version, "v0.0.0") {
		version = "master"
	}

	fs := osfs.New(filepath.Join(testDataPath("testdata"), version))
	repo, err := git.Init(memory.NewStorage(), fs)
	if err != nil {
		return nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	// Add all files in the directory.
	if _, err := wt.Add(""); err != nil {
		return nil, err
	}
	_, err = wt.Commit("", &git.CommitOptions{All: true, Author: &object.Signature{
		Name:  "Joe Random",
		Email: "joe@example.com",
		When:  testCommitTime,
	}})
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func TestVersionForTag(t *testing.T) {
	for _, test := range []struct {
		in, want string
	}{
		{"", ""},
		{"go1", "v1.0.0"},
		{"go1.9beta2", "v1.9.0-beta.2"},
		{"go1.12", "v1.12.0"},
		{"go1.9.7", "v1.9.7"},
		{"go2.0", "v2.0.0"},
		{"go1.9rc2", "v1.9.0-rc.2"},
		{"go1.1beta", ""},
		{"go1.0", ""},
		{"weekly.2012-02-14", ""},
		{"latest", "latest"},
	} {
		got := VersionForTag(test.in)
		if got != test.want {
			t.Errorf("VersionForTag(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestTagForVersion(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
		want    string
		wantErr bool
	}{
		{
			name:    "std version v1.0.0",
			version: "v1.0.0",
			want:    "go1",
		},
		{
			name:    "std version v1.12.5",
			version: "v1.12.5",
			want:    "go1.12.5",
		},
		{
			name:    "std version v1.13, incomplete canonical version",
			version: "v1.13",
			want:    "go1.13",
		},
		{
			name:    "std version v1.13.0-beta.1",
			version: "v1.13.0-beta.1",
			want:    "go1.13beta1",
		},
		{
			name:    "std version v1.9.0-rc.2",
			version: "v1.9.0-rc.2",
			want:    "go1.9rc2",
		},
		{
			name:    "std with digitless prerelease",
			version: "v1.13.0-prerelease",
			want:    "go1.13prerelease",
		},
		{
			name:    "version v1.13.0",
			version: "v1.13.0",
			want:    "go1.13",
		},
		{
			name:    "master branch",
			version: "master",
			want:    "master",
		},
		{
			name:    "master version",
			version: testVersion,
			want:    "master",
		},
		{
			name:    "bad std semver",
			version: "v1.x",
			wantErr: true,
		},
		{
			name:    "more bad std semver",
			version: "v1.0-",
			wantErr: true,
		},
		{
			name:    "bad prerelease",
			version: "v1.13.0-beta1",
			wantErr: true,
		},
		{
			name:    "another bad prerelease",
			version: "v1.13.0-whatevs99",
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := tagForVersion(test.version)
			if (err != nil) != test.wantErr {
				t.Errorf("tagForVersion(%q) = %q, %v, wantErr %v", test.version, got, err, test.wantErr)
				return
			}
			if got != test.want {
				t.Errorf("tagForVersion(%q) = %q, %v, wanted %q, %v", test.version, got, err, test.want, nil)
			}
		})
	}
}

func TestSemanticVersion(t *testing.T) {
	for _, test := range []struct {
		requestedVersion string
		want             string
	}{
		{
			requestedVersion: "latest",
			want:             "v1.14.6",
		},
		{
			requestedVersion: "master",
			want:             "master",
		},
	} {
		gotVersion, err := semanticVersion(testVersions, test.requestedVersion)
		if err != nil {
			t.Fatal(err)
		}
		if want := test.want; gotVersion != want {
			t.Errorf("version: got %q, want %q", gotVersion, want)
		}
	}
}

func TestDirectory(t *testing.T) {
	for _, test := range []struct {
		module  string
		version string
		want    string
	}{
		{
			module:  "archive",
			version: "v1.3.0-beta2",
			want:    "src/pkg/archive",
		},
		{
			module:  "bytes",
			version: "v1.16.0-beta1",
			want:    "src/bytes",
		},
		{
			module:  "io",
			version: "master",
			want:    "src/io",
		},
	} {
		got := directory(test.module, test.version)
		if got != test.want {
			t.Errorf("directory(%s) = %s, want %s", test.version, got, test.want)
		}
	}
}

func TestModuleFS(t *testing.T) {
	for _, test := range []struct {
		ModulePath string
		Versions   []string
		WantFiles  map[string]bool
	}{
		{
			ModulePath: "errors",
			Versions:   []string{"v1.14.6", "v1.12.5", "v1.3.2", testVersion},
			WantFiles: map[string]bool{
				"errors.go":      true,
				"errors_test.go": true,
			},
		},
		{
			ModulePath: "builtin",
			Versions:   []string{"v1.12.5"},
			WantFiles: map[string]bool{
				"builtin.go": true,
			},
		},
		{
			ModulePath: "flag",
			Versions:   []string{"v1.12.5"},
			WantFiles: map[string]bool{
				"example_test.go":       true,
				"example_value_test.go": true,
				"export_test.go":        true,
				"flag.go":               true,
				"flag_test.go":          true,
			},
		},
		{
			ModulePath: "cmd",
			Versions:   []string{"v1.14.6", testVersion},
		},
	} {
		for _, resolvedVersion := range test.Versions {
			t.Run(resolvedVersion, func(t *testing.T) {
				repo, err := getTestGoRepo(resolvedVersion)
				if err != nil {
					t.Fatal(err)
				}
				fsys, gotResolvedVersion, gotTime, err := moduleFS(repo, test.ModulePath, resolvedVersion)
				if err != nil {
					t.Fatal(err)
				}
				if resolvedVersion == "master" {
					if !module.IsPseudoVersion(gotResolvedVersion) {
						t.Errorf("resolved version: %s is not a pseudo-version", gotResolvedVersion)
					}
				} else if gotResolvedVersion != resolvedVersion {
					t.Errorf("resolved version: got %s, want %s", gotResolvedVersion, resolvedVersion)
				}
				if !gotTime.Equal(testCommitTime) {
					t.Errorf("commit time: got %s, want %s", gotTime, testCommitTime)
				}

				files, err := fs.ReadDir(fsys, ".")
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range files {
					delete(test.WantFiles, f.Name())
				}
				if len(test.WantFiles) > 0 {
					t.Errorf("module missing files: %v", reflect.ValueOf(test.WantFiles).MapKeys())
				}
			})
		}
	}
}
