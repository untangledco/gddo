package server

var (
	platforms = []string{
		"aix/ppc64",
		"android/amd64",
		"darwin/amd64",
		"dragonfly/amd64",
		"freebsd/amd64",
		"illumos/amd64",
		"ios/amd64",
		"js/wasm",
		"linux/amd64",
		"netbsd/amd64",
		"openbsd/amd64",
		"plan9/amd64",
		"solaris/amd64",
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
