package server

var (
	platforms = []string{
		"darwin/amd64",
		"js/wasm",
		"linux/amd64",
		"windows/amd64",
	}
	platformsMap = make(map[string]struct{})
)

func init() {
	for _, platform := range platforms {
		platformsMap[platform] = struct{}{}
	}
}

// validPlatform reports whether platform is a valid platform.
func validPlatform(platform string) bool {
	_, ok := platformsMap[platform]
	return ok
}
