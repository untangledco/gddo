//go:generate go run gen.go -output data.go

package platforms

import (
	"errors"
	"go/build"
	"path"
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
	goos, goarch := path.Split(platform)
	return &build.Context{
		GOOS:   goos,
		GOARCH: goarch,
	}, nil
}
