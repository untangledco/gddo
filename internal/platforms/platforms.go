//go:generate go run gen.go -output data.go

// Package platforms implements parsing and validation of platform strings.
package platforms

import (
	"errors"
	"go/build"
	"strings"
)

var ErrInvalid = errors.New("Invalid platform.")

// Valid reports whether platform is a valid platform.
func Valid(platform string) bool {
	_, ok := platforms[platform]
	return ok
}

// Parse parses platform and returns the corresponding build context.
func Parse(platform string) (*build.Context, error) {
	if !Valid(platform) {
		return nil, ErrInvalid
	}
	cut := strings.Index(platform, "/")
	if cut == -1 {
		return nil, ErrInvalid
	}
	goos, goarch := platform[:cut], platform[cut+1:]
	return &build.Context{
		GOOS:   goos,
		GOARCH: goarch,
	}, nil
}
