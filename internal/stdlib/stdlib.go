//go:generate go run gen.go -output data.go

// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package stdlib supports special handling of the Go standard library.
// Regardless of the how the standard library has been split into modules for
// development and testing, the discovery site treats it as a single module
// named "std".
package stdlib

import (
	"strings"

	"git.sr.ht/~sircmpwn/gddo/internal/proxy"
)

// Contains reports whether the given import path is part of the Go standard library.
func Contains(path string) bool {
	if path == proxy.StdlibModulePath {
		return true
	}
	path = strings.SplitN(path, "/", 2)[0]
	_, ok := stdlibPackagesMap[path]
	return ok
}
