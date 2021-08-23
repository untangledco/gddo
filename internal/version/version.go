// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package version handles version types.
package version

import (
	"fmt"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Type defines the version types a module can have.
type Type int

const (
	// TypeRelease is a normal release.
	TypeRelease Type = iota + 1

	// TypePrerelease is a version with a prerelease.
	TypePrerelease

	// TypePseudo appears to have a prerelease of the
	// form <commit date>-<commit hash>.
	TypePseudo
)

// ParseType returns the Type of a given a version.
func ParseType(version string) (Type, error) {
	if !semver.IsValid(version) {
		return 0, fmt.Errorf("ParseType(%q): invalid semver", version)
	}

	switch {
	case module.IsPseudoVersion(version):
		return TypePseudo, nil
	case semver.Prerelease(version) != "":
		return TypePrerelease, nil
	default:
		return TypeRelease, nil
	}
}
