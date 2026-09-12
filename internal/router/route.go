package router

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
)

type Protocol string

const (
	ProtocolHTTP Protocol = "http"
	ProtocolGRPC Protocol = "grpc"
)

type Route struct {
	Name            string
	Protocol        Protocol
	PathPrefix      string
	UpstreamURL     *url.URL
	RateLimit       *RateLimitPolicy
	StripPathPrefix bool
}

// RateLimitPolicy defines the local token-bucket settings for a route.
type RateLimitPolicy struct {
	RequestsPerSecond float64
	Burst             int
}

func (r Route) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("route name must not be empty")
	}
	if r.Protocol != ProtocolHTTP && r.Protocol != ProtocolGRPC {
		return fmt.Errorf("unsupported protocol %q", r.Protocol)
	}
	if !strings.HasPrefix(r.PathPrefix, "/") {
		return fmt.Errorf("path prefix %q must start with '/'", r.PathPrefix)
	}
	if strings.HasSuffix(r.PathPrefix, "/") && r.PathPrefix != "/" {
		return fmt.Errorf(
			"path prefix %q must not end with '/' unless it is '/'",
			r.PathPrefix,
		)
	}
	if r.UpstreamURL == nil {
		return errors.New("upstream URL must not be nil")
	}

	if r.UpstreamURL.Scheme != "http" && r.UpstreamURL.Scheme != "https" {
		return fmt.Errorf(
			"upstream URL scheme %q must be either 'http' or 'https'",
			r.UpstreamURL.Scheme,
		)
	}
	if strings.TrimSpace(r.UpstreamURL.Host) == "" {
		return errors.New("upstream URL host must not be empty")
	}
	if r.RateLimit != nil {
		if err := r.RateLimit.validate(); err != nil {
			return fmt.Errorf("rate limit policy: %w", err)
		}
	}
	return nil
}

func (p RateLimitPolicy) validate() error {
	if math.IsInf(p.RequestsPerSecond, 0) || math.IsNaN(p.RequestsPerSecond) {
		return errors.New("requests per second must be finite")
	}
	if p.RequestsPerSecond <= 0 {
		return errors.New("requests per second must be positive")
	}
	if p.Burst <= 0 {
		return errors.New("burst must be positive")
	}
	return nil
}
