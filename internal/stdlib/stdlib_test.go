// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stdlib

import (
	"reflect"
	"strings"
	"testing"

	"golang.org/x/mod/module"
)

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
			version: TestVersion,
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
			got, err := TagForVersion(test.version)
			if (err != nil) != test.wantErr {
				t.Errorf("TagForVersion(%q) = %q, %v, wantErr %v", test.version, got, err, test.wantErr)
				return
			}
			if got != test.want {
				t.Errorf("TagForVersion(%q) = %q, %v, wanted %q, %v", test.version, got, err, test.want, nil)
			}
		})
	}
}

var TestVersion = "v0.0.0-20190904010203-89fb59e2e920"

func TestZip(t *testing.T) {
	UseTestData = true
	defer func() { UseTestData = false }()

	for _, test := range []struct {
		ModulePath string
		Versions   []string
		WantFiles  map[string]bool
	}{
		{
			ModulePath: "errors",
			Versions:   []string{"v1.14.6", "v1.12.5", "v1.3.2", TestVersion},
			WantFiles: map[string]bool{
				"errors.go":      true,
				"errors_test.go": true,
			},
		},
		{
			ModulePath: "cmd",
			Versions:   []string{"v1.14.6", TestVersion},
		},
	} {
		for _, resolvedVersion := range test.Versions {
			t.Run(resolvedVersion, func(t *testing.T) {
				zr, gotResolvedVersion, gotTime, err := Zip(test.ModulePath, resolvedVersion)
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
				if !gotTime.Equal(TestCommitTime) {
					t.Errorf("commit time: got %s, want %s", gotTime, TestCommitTime)
				}

				wantPrefix := test.ModulePath + "@" + resolvedVersion + "/"
				for _, f := range zr.File {
					if !strings.HasPrefix(f.Name, wantPrefix) {
						t.Errorf("filename %q missing prefix %q", f.Name, wantPrefix)
						continue
					}
					delete(test.WantFiles, f.Name[len(wantPrefix):])
				}
				if len(test.WantFiles) > 0 {
					t.Errorf("zip missing files: %v", reflect.ValueOf(test.WantFiles).MapKeys())
				}
			})
		}
	}
}

func TestZipInfo(t *testing.T) {
	UseTestData = true
	defer func() { UseTestData = false }()

	for _, tc := range []struct {
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
		gotVersion, err := ZipInfo(tc.requestedVersion)
		if err != nil {
			t.Fatal(err)
		}
		if want := tc.want; gotVersion != want {
			t.Errorf("version: got %q, want %q", gotVersion, want)
		}
	}
}

func TestVersions(t *testing.T) {
	UseTestData = true
	defer func() { UseTestData = false }()

	got, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	gotmap := map[string]bool{}
	for _, g := range got {
		gotmap[g] = true
	}
	wants := []string{
		"v1.4.2",
		"v1.9.0-rc.1",
		"v1.11.0",
		"v1.13.0-beta.1",
	}
	for _, w := range wants {
		if !gotmap[w] {
			t.Errorf("missing %s", w)
		}
	}
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
		got := Directory(test.module, test.version)
		if got != test.want {
			t.Errorf("Directory(%s) = %s, want %s", test.version, got, test.want)
		}
	}
}
