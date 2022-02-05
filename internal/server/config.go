package server

import (
	"flag"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"
)

// Server configuration.
type Config struct {
	ShareDir        string
	AssetsDir       string
	TemplatesDir    string
	BrandName       string
	AdminName       string
	AdminEmail      string
	WebsiteIssues   string
	BindHTTP        string
	BindGemini      string
	Hostname        string
	CertsDir        string
	Database        string
	GoProxy         string
	GoModCache      string
	Platform        string
	UserAgent       string
	FetchTimeout    time.Duration
	RequestTimeout  time.Duration
	RefreshInterval time.Duration
	MaxAge          time.Duration
}

func (c *Config) FlagSet() *flag.FlagSet {
	defaultPlatform := path.Join(runtime.GOOS, runtime.GOARCH)
	assetsDir := path.Join(c.ShareDir, "assets")
	templatesDir := path.Join(c.ShareDir, "templates")

	flags := flag.NewFlagSet("default", flag.ExitOnError)
	flags.StringVar(&c.AssetsDir, "assets", assetsDir, "Assets directory")
	flags.StringVar(&c.TemplatesDir, "templates", templatesDir, "Templates directory")
	flags.StringVar(&c.BrandName, "brand-name", "GoDoc", "Brand name to use in templates")
	flags.StringVar(&c.AdminName, "admin-name", "", "Admin name to use in templates")
	flags.StringVar(&c.AdminEmail, "admin-email", "", "Admin email address to use in templates")
	flags.StringVar(&c.WebsiteIssues, "website-issues", "", "URL for website issues to use in templates")
	flags.StringVar(&c.BindHTTP, "http", "", "Listen for HTTP connections on this address")
	flags.StringVar(&c.BindGemini, "gemini", "", "Listen for Gemini connections on this address")
	flags.StringVar(&c.Hostname, "hostname", "", "Hostname to accept Gemini requests for")
	flags.StringVar(&c.CertsDir, "certs", "", "Directory to store Gemini TLS certificates")
	flags.StringVar(&c.Database, "db", "", "PostgreSQL database URL")
	flags.StringVar(&c.GoProxy, "goproxy", "", "Go module proxy")
	flags.StringVar(&c.GoModCache, "modcache", defaultModCache(), "Go module cache")
	flags.StringVar(&c.Platform, "platform", defaultPlatform, "Default platform to use for documentation")
	flags.StringVar(&c.UserAgent, "user-agent", "GoDocBot", "User agent to use for HTTP requests")
	flags.DurationVar(&c.FetchTimeout, "fetch-timeout", 20*time.Second, "Timeout for fetching documentation")
	flags.DurationVar(&c.RequestTimeout, "request-timeout", 20*time.Second, "Timeout for roundtripping an HTTP request")
	flags.DurationVar(&c.RefreshInterval, "refresh-interval", 0, "Time to sleep between refreshing modules in the background. Zero disables background refreshing.")
	flags.DurationVar(&c.MaxAge, "max-age", 24*time.Hour, "Refresh modules that haven't been updated for more than this age")
	return flags
}

func defaultModCache() string {
	b, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
