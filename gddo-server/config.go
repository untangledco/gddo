package main

import (
	"flag"
	"go/build"
	"path/filepath"
	"time"
)

// Server configuration.
type Config struct {
	AssetsDir      string
	BindHTTP       string
	BindGemini     string
	CertsDir       string
	Database       string
	GoProxy        string
	DefaultGOOS    string
	DefaultArch    string
	UserAgent      string
	GetTimeout     time.Duration
	DialTimeout    time.Duration
	RequestTimeout time.Duration
	CrawlInterval  time.Duration
	MaxAge         time.Duration
}

func (c *Config) FlagSet() *flag.FlagSet {
	assetsDir := filepath.Join(defaultBase("git.sr.ht/~sircmpwn/gddo/gddo-server"), "assets")

	flags := flag.NewFlagSet("default", flag.ExitOnError)
	flags.StringVar(&c.AssetsDir, "assets", assetsDir, "Assets directory")
	flags.StringVar(&c.BindHTTP, "http", "", "Listen for HTTP connections on this address")
	flags.StringVar(&c.BindGemini, "gemini", "", "Listen for Gemini connections on this address")
	flags.StringVar(&c.CertsDir, "certs", "", "Directory to store Gemini TLS certificates")
	flags.StringVar(&c.Database, "db", "postgres://localhost", "PostgreSQL database URL")
	flags.StringVar(&c.GoProxy, "goproxy", "https://proxy.golang.org", "Go module proxy")
	flags.StringVar(&c.DefaultGOOS, "goos", "linux", "Default GOOS to use for documentation")
	flags.StringVar(&c.DefaultArch, "arch", "amd64", "Default architecture to use for documentation")
	flags.StringVar(&c.UserAgent, "user-agent", "GoDocBot", "User agent to use for HTTP requests")
	flags.DurationVar(&c.GetTimeout, "get-timeout", 20*time.Second, "Timeout for HTTP GET requests")
	flags.DurationVar(&c.DialTimeout, "dial-timeout", 5*time.Second, "Timeout for dialing HTTP connections")
	flags.DurationVar(&c.RequestTimeout, "request-timeout", 20*time.Second, "Timeout for roundtripping an HTTP request")
	flags.DurationVar(&c.CrawlInterval, "crawl-interval", 0, "Time to sleep between package crawls. Zero disables crawling.")
	flags.DurationVar(&c.MaxAge, "max-age", 24*time.Hour, "Crawl modules that haven't been crawled for longer than this age")
	return flags
}

func defaultBase(path string) string {
	p, err := build.Default.Import(path, "", build.FindOnly)
	if err != nil {
		return "."
	}
	return p.Dir
}
