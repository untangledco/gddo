// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package version

import (
	"testing"
)

func TestParseVersionType(t *testing.T) {
	testCases := []struct {
		name, version   string
		wantVersionType Type
		wantErr         bool
	}{
		{
			name:            "pseudo major version",
			version:         "v1.0.0-20190311183353-d8887717615a",
			wantVersionType: TypePseudo,
		},
		{
			name:            "pseudo prerelease version",
			version:         "v1.2.3-pre.0.20190311183353-d8887717615a",
			wantVersionType: TypePseudo,
		},
		{
			name:            "pseudo minor version",
			version:         "v1.2.4-0.20190311183353-d8887717615a",
			wantVersionType: TypePseudo,
		},
		{
			name:            "pseudo version invalid",
			version:         "v1.2.3-20190311183353-d8887717615a",
			wantVersionType: TypePrerelease,
		},
		{
			name:            "valid release",
			version:         "v1.0.0",
			wantVersionType: TypeRelease,
		},
		{
			name:            "valid prerelease",
			version:         "v1.0.0-alpha.1",
			wantVersionType: TypePrerelease,
		},
		{
			name:            "invalid version",
			version:         "not_a_version",
			wantVersionType: 0,
			wantErr:         true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			if gotVt, err := ParseType(test.version); (test.wantErr == (err != nil)) && test.wantVersionType != gotVt {
				t.Errorf("parseVersionType(%v) = %v, want %v", test.version, gotVt, test.wantVersionType)
			}
		})
	}
}
