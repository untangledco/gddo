//go:generate go run gen.go -output data.go

// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stdlib supports special handling of the Go standard library.
package stdlib

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

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

// TagForVersion returns the Go standard library repository tag corresponding
// to semver. The Go tags differ from standard semantic versions in a few ways,
// such as beginning with "go" instead of "v".
func TagForVersion(version string) (string, error) {
	// Special case: master => master
	if version == "master" || strings.HasPrefix(version, "v0.0.0") {
		return "master", nil
	}

	// Special case: v1.0.0 => go1.
	if version == "v1.0.0" {
		return "go1", nil
	}
	if !semver.IsValid(version) {
		return "", fmt.Errorf("%w: requested version is not a valid semantic version: %q", proxy.ErrInvalidVersion, version)
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
				return "", fmt.Errorf("%w: final digits in a prerelease must follow a period", proxy.ErrInvalidVersion)
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

const (
	GoRepoURL       = "https://go.googlesource.com/go"
	GoSourceRepoURL = GoRepoURL
)

// UseTestData determines whether to really clone the Go repo, or use
// stripped-down versions of the repo from the testdata directory.
var UseTestData = false

// TestCommitTime is the time used for all commits when UseTestData is true.
var (
	TestCommitTime = time.Date(2019, 9, 4, 1, 2, 3, 0, time.UTC)
)

// getGoRepo returns a repo object for the Go repo at version.
func getGoRepo(version string) (*git.Repository, error) {
	var ref plumbing.ReferenceName
	if version == "master" {
		ref = plumbing.HEAD
	} else {
		tag, err := TagForVersion(version)
		if err != nil {
			return nil, err
		}
		ref = plumbing.NewTagReferenceName(tag)
	}
	return git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:           GoRepoURL,
		ReferenceName: ref,
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
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
		When:  TestCommitTime,
	}})
	if err != nil {
		return nil, err
	}
	return repo, nil
}

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

// Versions returns all the versions of Go that are relevant to the discovery
// site. These are all release versions (tags of the forms "goN.N" and
// "goN.N.N", where N is a number) and beta or rc versions (tags of the forms
// "goN.NbetaN" and "goN.N.NbetaN", and similarly for "rc" replacing "beta").
func Versions() ([]string, error) {
	var refNames []plumbing.ReferenceName
	if UseTestData {
		refNames = testRefs
	} else {
		re := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
			URLs: []string{GoRepoURL},
		})
		refs, err := re.List(&git.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			refNames = append(refNames, r.Name())
		}
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

// Directory returns the directory of the standard library relative to the repo root.
func Directory(modulePath, version string) string {
	if semver.Compare(version, "v1.4.0-beta.1") >= 0 ||
		version == "master" || strings.HasPrefix(version, "v0.0.0") {
		return path.Join("src", modulePath)
	}
	// For versions older than v1.4.0-beta.1, the stdlib is in src/pkg.
	return path.Join("src/pkg", modulePath)
}

// ZipInfo returns the proxy .info information for the module std.
func ZipInfo(requestedVersion string) (resolvedVersion string, err error) {
	resolvedVersion, err = semanticVersion(requestedVersion)
	if err != nil {
		return "", err
	}
	return resolvedVersion, nil
}

// Zip creates a module zip representing the given standard library module at the
// given version (which must have been resolved with ZipInfo) and returns a
// reader to it. It also returns the time of the commit for that version. The
// zip file is in module form, with each path prefixed by ModuleName + "@" +
// version.
//
// Normally, Zip returns the resolved version it was passed. If the resolved
// version is "master", Zip returns a semantic version for the branch.
//
// Zip reads the standard library at the Go repository tag corresponding to to
// the given semantic version.
func Zip(modulePath, resolvedVersion string) (_ *zip.Reader, resolvedVersion2 string, commitTime time.Time, err error) {
	// This code taken, with modifications, from
	// https://github.com/shurcooL/play/blob/master/256/moduleproxy/std/std.go.

	var repo *git.Repository
	if UseTestData {
		repo, err = getTestGoRepo(resolvedVersion)
	} else {
		repo, err = getGoRepo(resolvedVersion)
	}
	if err != nil {
		return nil, "", time.Time{}, err
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
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
	prefixPath := modulePath + "@" + resolvedVersion
	// Add files from the stdlib directory.
	libdir := root
	for _, d := range strings.Split(Directory(modulePath, resolvedVersion), "/") {
		libdir, err = subTree(repo, libdir, d)
		if err != nil {
			return nil, "", time.Time{}, err
		}
	}
	if err := addFiles(z, repo, libdir, prefixPath, true); err != nil {
		return nil, "", time.Time{}, err
	}
	if err := z.Close(); err != nil {
		return nil, "", time.Time{}, err
	}
	br := bytes.NewReader(buf.Bytes())
	zr, err := zip.NewReader(br, int64(br.Len()))
	if err != nil {
		return nil, "", time.Time{}, err
	}
	if resolvedVersion == "master" {
		resolvedVersion = newPseudoVersion("v0.0.0", commit.Committer.When, commit.Hash)
	}
	return zr, resolvedVersion, commit.Committer.When, nil
}

func newPseudoVersion(version string, commitTime time.Time, hash plumbing.Hash) string {
	return fmt.Sprintf("%s-%s-%s", version, commitTime.Format("20060102150405"), hash.String()[:12])
}

// semanticVersion returns the semantic version corresponding to the
// requestedVersion. If the requested version is "master", then semanticVersion
// returns it as is. The branch name is resolved to a proper pseudo-version in
// Zip.
func semanticVersion(requestedVersion string) (string, error) {
	if requestedVersion == "master" {
		return "master", nil
	}

	knownVersions, err := Versions()
	if err != nil {
		return "", err
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

	return "", fmt.Errorf("%w: requested version unknown: %q", proxy.ErrNotFound, requestedVersion)
}

// addFiles adds the files in t to z, using dirpath as the path prefix.
// If recursive is true, it also adds the files in all subdirectories.
func addFiles(z *zip.Writer, r *git.Repository, t *object.Tree, dirpath string, recursive bool) error {
	for _, e := range t.Entries {
		if strings.HasPrefix(e.Name, ".") || strings.HasPrefix(e.Name, "_") {
			continue
		}
		if e.Name == "go.mod" {
			// ignore; we'll synthesize our own
			continue
		}
		switch e.Mode {
		case filemode.Regular, filemode.Executable:
			blob, err := r.BlobObject(e.Hash)
			if err != nil {
				return err
			}
			src, err := blob.Reader()
			if err != nil {
				return err
			}
			if err := writeZipFile(z, path.Join(dirpath, e.Name), src); err != nil {
				_ = src.Close()
				return err
			}
			if err := src.Close(); err != nil {
				return err
			}
		case filemode.Dir:
			if !recursive || e.Name == "testdata" {
				continue
			}
			t2, err := r.TreeObject(e.Hash)
			if err != nil {
				return err
			}
			if err := addFiles(z, r, t2, path.Join(dirpath, e.Name), recursive); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeZipFile(z *zip.Writer, pathname string, src io.Reader) error {
	dst, err := z.Create(pathname)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}

// subTree looks non-recursively for a directory with the given name in t,
// and returns the corresponding tree.
// If a directory with such name doesn't exist in t, it returns os.ErrNotExist.
func subTree(r *git.Repository, t *object.Tree, name string) (*object.Tree, error) {
	for _, e := range t.Entries {
		if e.Name == name {
			return r.TreeObject(e.Hash)
		}
	}
	return nil, os.ErrNotExist
}

// Contains reports whether the given import path is part of the Go standard library.
func Contains(path string) bool {
	_, ok := stdlibPackagesMap[path]
	return ok
}

// Packages returns a list of packages in the standard library.
func Packages() []string {
	return stdlibPackages
}

// References used for Versions during testing.
var testRefs = []plumbing.ReferenceName{
	// stdlib versions
	"refs/tags/go1.2.1",
	"refs/tags/go1.3.2",
	"refs/tags/go1.4.2",
	"refs/tags/go1.4.3",
	"refs/tags/go1.6",
	"refs/tags/go1.6.3",
	"refs/tags/go1.6beta1",
	"refs/tags/go1.8",
	"refs/tags/go1.8rc2",
	"refs/tags/go1.9rc1",
	"refs/tags/go1.11",
	"refs/tags/go1.12",
	"refs/tags/go1.12.1",
	"refs/tags/go1.12.5",
	"refs/tags/go1.12.9",
	"refs/tags/go1.13",
	"refs/tags/go1.13beta1",
	"refs/tags/go1.14.6",
	"refs/heads/master",
	// other tags
	"refs/changes/56/93156/13",
	"refs/tags/release.r59",
	"refs/tags/weekly.2011-04-13",
}
