//go:generate go run gen.go -output data.go

// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stdlib supports special handling of the Go standard library.
package stdlib

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// Contains reports whether the given import path is part of the Go standard library.
func Contains(path string) bool {
	_, ok := stdlibPackagesMap[path]
	return ok
}

// Packages returns a list of packages in the standard library.
func Packages() []string {
	return stdlibPackages
}

// Module fetches a standard library module from the Go git repository.
func Module(modulePath, version string) (*internal.Module, error) {
	versions, err := versions()
	if err != nil {
		return nil, err
	}

	// Get version info
	version, err = semanticVersion(versions, version)
	if err != nil {
		return nil, err
	}
	latestVersion, err := semanticVersion(versions, internal.LatestVersion)
	if err != nil {
		return nil, err
	}

	seriesPath, _, _ := module.SplitPathVersion(modulePath)

	return &internal.Module{
		ModulePath:    modulePath,
		SeriesPath:    seriesPath,
		Version:       version,
		LatestVersion: latestVersion,
		Versions:      versions,
	}, nil
}

// Files returns the standard library module's files.
func Files(mod *internal.Module) (fs.FS, error) {
	repo, err := getGoRepo(mod.Version)
	if err != nil {
		return nil, err
	}
	// Get filesystem
	fsys, version, commitTime, err := moduleFS(repo, mod.ModulePath, mod.Version)
	if err != nil {
		return nil, err
	}
	// Update version
	mod.Version = version
	mod.CommitTime = commitTime
	return fsys, nil
}

const (
	goRepoURL = "https://go.googlesource.com/go"
)

// getGoRepo returns a repo object for the Go repo at version.
func getGoRepo(version string) (*git.Repository, error) {
	var ref plumbing.ReferenceName
	if version == "master" {
		ref = plumbing.HEAD
	} else {
		tag, err := tagForVersion(version)
		if err != nil {
			return nil, err
		}
		ref = plumbing.NewTagReferenceName(tag)
	}
	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:           goRepoURL,
		ReferenceName: ref,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if errors.Is(err, git.NoMatchingRefSpecError{}) {
		// Not found
		err = fmt.Errorf("%w: %v", internal.ErrNotFound, err)
	}
	return repo, err
}

// versions returns all the versions of Go that are relevant to the discovery
// site. These are all release versions (tags of the forms "goN.N" and
// "goN.N.N", where N is a number) and beta or rc versions (tags of the forms
// "goN.NbetaN" and "goN.N.NbetaN", and similarly for "rc" replacing "beta").
func versions() ([]string, error) {
	re := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		URLs: []string{goRepoURL},
	})
	refs, err := re.List(&git.ListOptions{})
	if err != nil {
		return nil, err
	}
	var refNames []plumbing.ReferenceName
	for _, r := range refs {
		refNames = append(refNames, r.Name())
	}

	var versions []string
	for _, name := range refNames {
		v := VersionForTag(name.Short())
		if v != "" {
			versions = append(versions, v)
		}
	}
	return versions, nil
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

// VersionForTag returns the semantic version for the Go tag, or "" if
// tag doesn't correspond to a Go release or beta tag. In special cases,
// when the tag specified is either `latest` or `master` it will return the tag.
// Examples:
//   "go1" => "v1.0.0"
//   "go1.2" => "v1.2.0"
//   "go1.13beta1" => "v1.13.0-beta.1"
//   "go1.9rc2" => "v1.9.0-rc.2"
//   "latest" => "latest"
//   "master" => "master"
func VersionForTag(tag string) string {
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
func tagForVersion(version string) (string, error) {
	// Special case: master => master
	if version == "master" || strings.HasPrefix(version, "v0.0.0") {
		return "master", nil
	}

	// Special case: v1.0.0 => go1.
	if version == "v1.0.0" {
		return "go1", nil
	}
	if !semver.IsValid(version) {
		return "", fmt.Errorf("%w: requested version is not a valid semantic version: %q", internal.ErrInvalidVersion, version)
	}
	goVersion := semver.Canonical(version)
	prerelease := semver.Prerelease(goVersion)
	versionWithoutPrerelease := strings.TrimSuffix(goVersion, prerelease)
	patch := strings.TrimPrefix(versionWithoutPrerelease, semver.MajorMinor(goVersion)+".")
	if patch == "0" {
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

// semanticVersion returns the semantic version corresponding to the
// requestedVersion. If the requested version is "master", then semanticVersion
// returns it as is. The branch name is resolved to a proper pseudo-version in
// moduleZip.
func semanticVersion(knownVersions []string, requestedVersion string) (string, error) {
	if requestedVersion == "master" {
		return "master", nil
	}

	switch requestedVersion {
	case "latest":
		var latestVersion string
		for _, v := range knownVersions {
			if !strings.HasPrefix(v, "v") {
				continue
			}
			if module.IsPseudoVersion(v) || semver.Prerelease(v) != "" {
				// We expect there to always be at least 1 release version.
				continue
			}
			if semver.Compare(v, latestVersion) > 0 {
				latestVersion = v
			}
		}
		return latestVersion, nil
	default:
		for _, v := range knownVersions {
			if v == requestedVersion {
				return requestedVersion, nil
			}
		}
	}

	return "", fmt.Errorf("%w: requested version unknown: %q", internal.ErrNotFound, requestedVersion)
}

// directory returns the directory of the standard library relative to the repo root.
func directory(modulePath, version string) string {
	if semver.Compare(version, "v1.4.0-beta.1") >= 0 ||
		version == "master" || strings.HasPrefix(version, "v0.0.0") {
		return path.Join("src", modulePath)
	}
	// For versions older than v1.4.0-beta.1, the stdlib is in src/pkg.
	return path.Join("src/pkg", modulePath)
}

// moduleFS returns a filesystem containing the source code of the given
// standard library module at the given version. It also returns the time of
// the commit for that version.
//
// Normally, moduleFS returns the resolved version it was passed. If the resolved
// version is "master", moduleFS returns a semantic version for the branch.
//
// moduleFS reads the standard library at the Go repository tag corresponding to to
// the given semantic version.
func moduleFS(repo *git.Repository, modulePath, resolvedVersion string) (_ fs.FS, resolvedVersion2 string, commitTime time.Time, err error) {
	head, err := repo.Head()
	if err != nil {
		return nil, "", time.Time{}, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, "", time.Time{}, err
	}
	if resolvedVersion == "master" {
		resolvedVersion = newPseudoVersion("v0.0.0", commit.Committer.When, commit.Hash)
	}
	root, err := repo.TreeObject(commit.TreeHash)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	// Add files from the stdlib directory.
	tree := root
	for _, d := range strings.Split(directory(modulePath, resolvedVersion), "/") {
		tree, err = subTree(repo, tree, d)
		if err != nil {
			return nil, "", time.Time{}, err
		}
	}
	// Create filesystem
	fsys, err := newGitFS(repo, tree)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return fsys, resolvedVersion, commit.Committer.When, nil
}

// subTree looks non-recursively for a directory with the given name in t,
// and returns the corresponding tree.
// If a directory with such name doesn't exist in t, it returns ErrNotFound.
func subTree(r *git.Repository, t *object.Tree, name string) (*object.Tree, error) {
	for _, e := range t.Entries {
		if e.Name == name {
			return r.TreeObject(e.Hash)
		}
	}
	return nil, internal.ErrNotFound
}

func newPseudoVersion(version string, commitTime time.Time, hash plumbing.Hash) string {
	return fmt.Sprintf("%s-%s-%s", version, commitTime.Format("20060102150405"), hash.String()[:12])
}
