package source

import (
	"archive/zip"
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"sort"
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
	"git.sr.ht/~sircmpwn/gddo/internal/stdlib"
	"golang.org/x/mod/modfile"
)

// Get retrieves the source code for a module from a module proxy.
func Get(ctx context.Context, client *proxy.Client, modulePath, version string) (*Module, error) {
	if modulePath == stdlib.ModulePath {
		return getStdlib(version)
	}

	// Get version info
	info, err := client.GetInfo(ctx, modulePath, version)
	if err != nil {
		return nil, err
	}
	// Get module file
	mod, err := client.GetMod(ctx, modulePath, info.Version)
	if err != nil {
		return nil, err
	}
	// Get module path
	if path := modfile.ModulePath(mod); path != "" {
		modulePath = path
	}
	// Get module zip
	zip, err := client.GetZip(ctx, modulePath, info.Version)
	if err != nil {
		return nil, err
	}
	// Parse packages
	pkgs, err := parsePackages(zip, modulePath, info.Version)
	if err != nil {
		return nil, err
	}
	return &Module{
		Path:     modulePath,
		Version:  info.Version,
		Time:     info.Time,
		Packages: pkgs,
	}, nil
}

func getStdlib(version string) (*Module, error) {
	// Get zip
	zip, version, time, err := stdlib.Zip(version)
	if err != nil {
		return nil, err
	}
	// Parse packages
	pkgs, err := parsePackages(zip, stdlib.ModulePath, version)
	if err != nil {
		return nil, err
	}
	return &Module{
		Path:     stdlib.ModulePath,
		Version:  version,
		Time:     time,
		Packages: pkgs,
	}, nil
}

// isSourceFile reports whether a file with name n should be included in the source.
func isSourceFile(n string) bool {
	return strings.HasSuffix(n, ".go") && n[0] != '_' && n[0] != '.'
}

// parsePackages parses packages from the provided zip reader.
func parsePackages(zip *zip.Reader, modulePath, version string) ([]*Package, error) {
	prefix := fmt.Sprintf("%s@%s/", modulePath, version)

	pkgsMap := map[string]*Package{}
	for _, file := range zip.File {
		pkgPath, name := path.Split(file.Name)
		pkgPath = strings.TrimPrefix(pkgPath, prefix)
		pkgPath = strings.TrimSuffix(pkgPath, "/")

		if ignoredByGoTool(pkgPath) || isVendored(pkgPath) {
			// Skip ignored paths
			continue
		}
		if !strings.HasSuffix(name, ".go") {
			// We care about .go files only.
			continue
		}

		// Read file
		open, err := file.Open()
		if err != nil {
			return nil, err
		}
		b, err := ioutil.ReadAll(open)
		if err != nil {
			return nil, err
		}

		// Add parent packages if they don't exist
		dir := path.Dir(pkgPath)
		for {
			var stop bool
			if dir == "." {
				dir = ""
				stop = true
			}
			_, ok := pkgsMap[dir]
			if ok {
				break
			}
			var importPath string
			if modulePath == stdlib.ModulePath {
				importPath = dir
			} else {
				importPath = path.Join(modulePath, dir)
			}
			pkgsMap[dir] = &Package{
				Path: importPath,
			}
			if stop {
				break
			}
			dir = path.Dir(dir)
		}

		// Add package if it does not exist
		pkg, ok := pkgsMap[pkgPath]
		if !ok {
			var importPath string
			if modulePath == stdlib.ModulePath {
				importPath = pkgPath
			} else {
				importPath = path.Join(modulePath, pkgPath)
			}
			pkg = &Package{
				Path: importPath,
			}
			pkgsMap[pkgPath] = pkg
		}

		// Add file
		pkg.Files = append(pkg.Files, &File{
			Name:     name,
			Contents: b,
		})
	}

	// Sort packages by path
	var pkgs []*Package
	for _, pkg := range pkgsMap {
		pkgs = append(pkgs, pkg)
	}
	sort.Sort(byPath(pkgs))
	return pkgs, nil
}

type byPath []*Package

func (s byPath) Len() int           { return len(s) }
func (s byPath) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byPath) Less(i, j int) bool { return s[i].Path < s[j].Path }

// ignoredByGoTool reports whether the given import path corresponds
// to a directory that would be ignored by the go tool.
//
// The logic of the go tool for ignoring directories is documented at
// https://golang.org/cmd/go/#hdr-Package_lists_and_patterns:
//
// 	Directory and file names that begin with "." or "_" are ignored
// 	by the go tool, as are directories named "testdata".
//
// However, even though `go list` and other commands that take package
// wildcards will ignore these, they can still be imported and used in
// working Go programs. We continue to ignore the "." and "testdata"
// cases, but we've seen valid Go packages with "_", so we accept those.
func ignoredByGoTool(importPath string) bool {
	for _, el := range strings.Split(importPath, "/") {
		if strings.HasPrefix(el, ".") || el == "testdata" {
			return true
		}
	}
	return false
}

// isVendored reports whether the given import path corresponds
// to a Go package that is inside a vendor directory.
//
// The logic for what is considered a vendor directory is documented at
// https://golang.org/cmd/go/#hdr-Vendor_Directories.
func isVendored(importPath string) bool {
	return strings.HasPrefix(importPath, "vendor/") ||
		strings.Contains(importPath, "/vendor/")
}
