//go:generate go run gen.go -output data.go

// Package platforms implements parsing and validation of platform strings.
package platforms

// Platforms returns a list of valid platforms.
func Platforms() []string {
	return platforms
}

// Valid reports whether platform is a valid platform.
func Valid(platform string) bool {
	_, ok := platformsMap[platform]
	return ok
}
