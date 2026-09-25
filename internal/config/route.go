package config

import "time"

// RouteConfig describes one gateway route loaded from the configuration file.
type RouteConfig struct {
	Name                string                 `yaml:"name"`
	Protocol            string                 `yaml:"protocol"`
	PathPrefix          string                 `yaml:"path_prefix"`
	PathExact           string                 `yaml:"path_exact"`
	HeaderMatches       []HeaderMatchConfig    `yaml:"header_matches"`
	Methods             []string               `yaml:"methods"`
	Hosts               []string               `yaml:"hosts"`
	Priority            int                    `yaml:"priority"`
	UpstreamURL         string                 `yaml:"upstream_url"`
	DirectResponse      *DirectResponseConfig  `yaml:"direct_response"`
	RateLimit           *RateLimitConfig       `yaml:"rate_limit"`
	StripPathPrefix     bool                   `yaml:"strip_path_prefix"`
	RequestTimeout      time.Duration          `yaml:"request_timeout"`
	MaxRequestBodyBytes int64                  `yaml:"max_request_body_bytes"`
	RequestHeaders      *HeaderTransformConfig `yaml:"request_headers"`
	ResponseHeaders     *HeaderTransformConfig `yaml:"response_headers"`
}

// HeaderTransformConfig describes static header values to set and header names
// to remove when applying a route's header policy.
type HeaderTransformConfig struct {
	Set    map[string]string `yaml:"set"`
	Remove []string          `yaml:"remove"`
}

// RateLimitConfig configures a local token-bucket rate limiter for a route.
type RateLimitConfig struct {
	RequestsPerSecond float64 `yaml:"requests_per_second"`
	Burst             int     `yaml:"burst"`
}

// DirectResponseConfig configures a static HTTP response returned without
// forwarding the request to an upstream.
type DirectResponseConfig struct {
	StatusCode int    `yaml:"status"`
	Body       string `yaml:"body"`
}

// HeaderMatchConfig describes an exact request header condition for a route.
type HeaderMatchConfig struct {
	Name  string `yaml:"name"`
	Exact string `yaml:"exact"`
}

type fileConfig struct {
	Routes []RouteConfig `yaml:"routes"`
}
