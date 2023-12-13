// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import "testing"

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
		got := versionForTag(test.in)
		if got != test.want {
			t.Errorf("versionForTag(%q) = %q, want %q", test.in, got, test.want)
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
			version: "v0.0.0-20190904010203-89fb59e2e920",
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

func TestStdlibDir(t *testing.T) {
	for _, test := range []struct {
		version string
		want    string
	}{
		{
			version: "v1.3.0-beta2",
			want:    "src/pkg",
		},
		{
			version: "v1.16.0-beta1",
			want:    "src",
		},
		{
			version: "master",
			want:    "src",
		},
	} {
		got := stdlibDir(test.version)
		if got != test.want {
			t.Errorf("directory(%s) = %s, want %s", test.version, got, test.want)
		}
	}
}
