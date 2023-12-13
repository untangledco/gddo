package proxy

import (
	"fmt"
	"regexp"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"golang.org/x/mod/semver"
)

const (
	// The Go toolchain module path
	StdlibModulePath = "std"

	// The Go toolchain module path
	ToolchainModulePath = "golang.org/toolchain"
)

// toolchainVersion returns the Go toolchain version for the given semantic version.
func toolchainVersion(v string) (string, error) {
	if v == internal.LatestVersion {
		return v, nil
	}
	v, err := tagForVersion(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v0.0.1-%s.linux-amd64", v), nil
}

// stdlibTag returns the Go standard library repository tag for the given
// Go toolchain version.
func stdlibTag(v string) string {
	v = v[:strings.LastIndex(v, ".")]
	return strings.TrimPrefix(v, "v0.0.1-")
}

func stdlibVersions(vs []string) []string {
	result := []string{}
	for i := 0; i < len(vs); i++ {
		if !strings.HasSuffix(vs[i], ".linux-amd64") {
			continue
		}
		result = append(result, versionForTag(stdlibTag(vs[i])))
	}
	return result
}

// stdlibDir returns the directory of the standard library relative to the toolchain root.
func stdlibDir(version string) string {
	if semver.Compare(version, "v1.4.0-beta.1") >= 0 ||
		version == "master" || strings.HasPrefix(version, "v0.0.0") {
		return "src"
	}
	// For versions older than v1.4.0-beta.1, the stdlib is in src/pkg.
	return "src/pkg"
}

var (
	// Regexp for matching go tags. The groups are:
	// 1  the major.minor version
	// 2  the patch version, or empty if none
	// 3  the entire prerelease, if present
	// 4  the prerelease type ("beta" or "rc")
	// 5  the prerelease number
	tagRegexp = regexp.MustCompile(`^go(\d+\.\d+)(\.\d+|)((beta|rc)(\d+))?$`)
)

// versionForTag returns the semantic version for the Go tag, or "" if
// tag doesn't correspond to a Go release or beta tag. In special cases,
// when the tag specified is either `latest` or `master` it will return the tag.
// Examples:
//
//	"go1" => "v1.0.0"
//	"go1.2" => "v1.2.0"
//	"go1.13beta1" => "v1.13.0-beta.1"
//	"go1.9rc2" => "v1.9.0-rc.2"
//	"latest" => "latest"
//	"master" => "master"
func versionForTag(tag string) string {
	// Special cases for go1.
	if tag == "go1" {
		return "v1.0.0"
	}
	if tag == "go1.0" {
		return ""
	}
	// Special case for latest and master.
	if tag == "latest" || tag == "master" {
		return tag
	}
	m := tagRegexp.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	version := "v" + m[1]
	if m[2] != "" {
		version += m[2]
	} else {
		version += ".0"
	}
	if m[3] != "" {
		version += "-" + m[4] + "." + m[5]
	}
	return version
}

// tagForVersion returns the Go standard library repository tag corresponding
// to semver. The Go tags differ from standard semantic versions in a few ways,
// such as beginning with "go" instead of "v".
//
// Starting with go1.21.0, the first patch release of major go versions include
// the .0 suffix. Previously, the .0 suffix was elided (golang/go#57631).
func tagForVersion(v string) (string, error) {
	// Special case: master => master
	if v == "master" {
		return v, nil
	}
	if strings.HasPrefix(v, "v0.0.0") {
		return "master", nil
	}
	// Special case: v1.0.0 => go1.
	if v == "v1.0.0" {
		return "go1", nil
	}
	if !semver.IsValid(v) {
		return "", fmt.Errorf("%w: requested version is not a valid semantic version: %q ", internal.ErrInvalidVersion, v)
	}
	goVersion := semver.Canonical(v)
	prerelease := semver.Prerelease(goVersion)
	versionWithoutPrerelease := strings.TrimSuffix(goVersion, prerelease)
	patch := strings.TrimPrefix(versionWithoutPrerelease, semver.MajorMinor(goVersion)+".")
	if patch == "0" && (semver.Compare(v, "v1.21.0") < 0 || prerelease != "") {
		// Starting with go1.21.0, the first patch version includes .0.
		// Prereleases do not include .0 (we don't do prereleases for other patch releases).
		versionWithoutPrerelease = strings.TrimSuffix(versionWithoutPrerelease, ".0")
	}
	goVersion = fmt.Sprintf("go%s", strings.TrimPrefix(versionWithoutPrerelease, "v"))
	if prerelease != "" {
		// Go prereleases look like  "beta1" instead of "beta.1".
		// "beta1" is bad for sorting (since beta10 comes before beta9), so
		// require the dot form.
		i := finalDigitsIndex(prerelease)
		if i >= 1 {
			if prerelease[i-1] != '.' {
				return "", fmt.Errorf("%w: final digits in a prerelease must follow a period", internal.ErrInvalidVersion)
			}
			// Remove the dot.
			prerelease = prerelease[:i-1] + prerelease[i:]
		}
		goVersion += strings.TrimPrefix(prerelease, "-")
	}
	return goVersion, nil
}

// finalDigitsIndex returns the index of the first digit in the sequence of digits ending s.
// If s doesn't end in digits, it returns -1.
func finalDigitsIndex(s string) int {
	// Assume ASCII (since the semver package does anyway).
	var i int
	for i = len(s) - 1; i >= 0; i-- {
		if s[i] < '0' || s[i] > '9' {
			break
		}
	}
	if i == len(s)-1 {
		return -1
	}
	return i + 1
}
