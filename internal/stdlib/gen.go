// +build ignore

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

const template = `// Automatically generated by go generate. DO NOT EDIT

package stdlib

var stdlibPackages = %#v

var stdlibPackagesMap = %#v
`

func main() {
	output := flag.String("output", "data.go", "output file")
	flag.Parse()

	pkgsMap := map[string]struct{}{}
	cmd := exec.Command("go", "list", "std", "cmd")
	o, err := cmd.Output()
	if err != nil {
		log.Fatal(err)
	}
	for _, pkg := range strings.Fields(string(o)) {
		if strings.HasPrefix(pkg, "vendor/") || strings.Contains(pkg, "/vendor/") {
			continue
		}
		// Add the package and all of its parent directories
		for ; pkg != "."; pkg = path.Dir(pkg) {
			pkgsMap[pkg] = struct{}{}
		}
	}

	pkgs := []string{}
	for pkg := range pkgsMap {
		// Hide cmd and internal packages from the list
		if strings.HasPrefix(pkg, "cmd/") ||
			strings.HasPrefix(pkg, "internal/") ||
			strings.Contains(pkg, "/internal/") {
			continue
		}
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	f, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	fmt.Fprintf(f, template, pkgs, pkgsMap)
}
