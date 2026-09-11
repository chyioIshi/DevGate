package router

import "net/url"

type Protocol string

const (
	ProtocolHTTP Protocol = "http"
	ProtocolGRPC Protocol = "grpc"
)

type Route struct {
	Name        string
	Protocol    Protocol
	PathPrefix  string
	UpstreamURL *url.URL
	RateLimit   *RateLimitPolicy
}

// RateLimitPolicy defines the local token-bucket settings for a route.
type RateLimitPolicy struct {
	RequestsPerSecond float64
	Burst             int
}
