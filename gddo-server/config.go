package main

import (
	"context"
	"fmt"
	"go/build"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	ConfigGoProxy           = "goproxy"
	ConfigTrustProxyHeaders = "trust-proxy-headers"
	ConfigBindAddress       = "http"
	ConfigAssetsDir         = "assets"
	ConfigPGServer          = "pg-server"
	ConfigUserAgent         = "user-agent"

	// Display Config
	ConfigDefaultGOOS = "default-goos"

	// Crawl Config
	ConfigMaxAge          = "max-age"
	ConfigGetTimeout      = "get-timeout"
	ConfigFirstGetTimeout = "first-get-timeout"
	ConfigCrawlInterval   = "crawl-interval"
	ConfigDialTimeout     = "dial-timeout"
	ConfigRequestTimeout  = "request-timeout"
)

func defaultBase(path string) string {
	p, err := build.Default.Import(path, "", build.FindOnly)
	if err != nil {
		return "."
	}
	return p.Dir
}

func loadConfig(ctx context.Context, args []string) (*viper.Viper, error) {
	v := viper.New()

	// Setup command line flags
	flags := buildFlags()
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if err := v.BindPFlags(flags); err != nil {
		return nil, err
	}

	// Also fetch from environment
	v.SetEnvPrefix("gddo")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()

	// Read from config.
	if err := readViperConfig(ctx, v); err != nil {
		return nil, err
	}

	log.Println("configuration values:")
	settings := v.AllSettings()
	for k, v := range settings {
		log.Printf("  %s = %v\n", k, v)
	}

	return v, nil
}

func buildFlags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("default", pflag.ContinueOnError)

	flags.StringP("config", "c", "", "path to motd config file")
	flags.String(ConfigAssetsDir, filepath.Join(defaultBase("github.com/golang/gddo/gddo-server"), "assets"), "Base directory for templates and static files.")
	flags.Duration(ConfigGetTimeout, 8*time.Second, "Time to wait for package update from the VCS.")
	flags.Duration(ConfigFirstGetTimeout, 5*time.Second, "Time to wait for first fetch of package from the VCS.")
	flags.Duration(ConfigMaxAge, 24*time.Hour, "Update package documents older than this age.")
	flags.String(ConfigBindAddress, ":8080", "Listen for HTTP connections on this address.")
	flags.String(ConfigDefaultGOOS, "", "Default GOOS to use when building package documents.")
	flags.Bool(ConfigTrustProxyHeaders, false, "If enabled, identify the remote address of the request using X-Real-Ip in header.")
	flags.Duration(ConfigCrawlInterval, 0, "Package updater sleeps for this duration between package updates. Zero disables updates.")
	flags.Duration(ConfigDialTimeout, 5*time.Second, "Timeout for dialing an HTTP connection.")
	flags.Duration(ConfigRequestTimeout, 20*time.Second, "Time out for roundtripping an HTTP request.")
	flags.String(ConfigPGServer, "", "URI of PostgreSQL server (for full text search).")
	flags.String(ConfigGoProxy, "https://proxy.golang.org", "Go module proxy.")

	return flags
}

// readViperConfig finds and then parses a config file. It will return
// an error if the config file was specified or could not parse.
// Otherwise it will only warn that it failed to load the config.
func readViperConfig(ctx context.Context, v *viper.Viper) error {
	v.AddConfigPath(".")
	v.AddConfigPath("/etc")
	v.SetConfigName("gddo")
	if v.GetString("config") != "" {
		v.SetConfigFile(v.GetString("config"))
	}

	if err := v.ReadInConfig(); err != nil {
		// If a config exists but could not be parsed, we should bail.
		if _, ok := err.(viper.ConfigParseError); ok {
			return fmt.Errorf("parse config: %v", err)
		}

		// If the user specified a config file location in flags or env and
		// we failed to load it, we should bail. If not, it is just a warning.
		if v.GetString("config") != "" {
			return fmt.Errorf("load config: %v", err)
		}
		log.Println("failed to load configuration file:", err)
		return nil
	}
	log.Println("succesfully loaded configuration from", v.ConfigFileUsed())
	return nil
}
