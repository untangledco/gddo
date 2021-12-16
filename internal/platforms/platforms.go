//go:generate go run gen.go -output data.go

// Package platforms implements parsing and validation of platform strings.
package platforms

// Valid reports whether platform is a valid platform.
func Valid(platform string) bool {
	_, ok := platforms[platform]
	return ok
}
