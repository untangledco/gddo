package database

import (
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/doc"
)

// searchScore calculates the search searchScore for the provided package documentation.
func searchScore(pkg *doc.Package) float64 {
	// Ignore internal packages
	if pkg.Name == "" ||
		strings.HasSuffix(pkg.ImportPath, "/internal") ||
		strings.Contains(pkg.ImportPath, "/internal/") {
		return 0
	}

	r := 1.0
	if pkg.IsCommand {
		if pkg.Doc == "" {
			// Do not include command in index if it does not have documentation.
			return 0
		}
	} else {
		if len(pkg.Consts) == 0 &&
			len(pkg.Vars) == 0 &&
			len(pkg.Funcs) == 0 &&
			len(pkg.Types) == 0 &&
			len(pkg.Examples) == 0 {
			// Do not include package in index if it does not have exports.
			return 0
		}
		if pkg.Doc == "" {
			// Penalty for no documentation.
			r *= 0.95
		}
	}
	return r
}
