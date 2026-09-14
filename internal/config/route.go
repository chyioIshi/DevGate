package config

import "time"

// RouteConfig describes one gateway route loaded from the configuration file.
type RouteConfig struct {
	Name            string           `yaml:"name"`
	Protocol        string           `yaml:"protocol"`
	PathPrefix      string           `yaml:"path_prefix"`
	UpstreamURL     string           `yaml:"upstream_url"`
	RateLimit       *RateLimitConfig `yaml:"rate_limit"`
	StripPathPrefix bool             `yaml:"strip_path_prefix"`
	RequestTimeout  time.Duration    `yaml:"request_timeout"`
}

// RateLimitConfig configures a local token-bucket rate limiter for a route.
type RateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

type fileConfig struct {
	Routes []RouteConfig `yaml:"routes"`
}
