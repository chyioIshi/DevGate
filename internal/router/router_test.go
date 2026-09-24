package router

import (
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		routes      []Route
		wantMessage string
	}{
		{
			name: "valid HTTP route",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET", "POST", "PURGE"},
					Hosts:       []string{"api.example.com", "api-v1.example.com", "localhost", "127.0.0.1"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
		},
		{
			name: "valid exact path with trailing slash",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users/",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
		},
		{
			name: "valid HTTP gRPC and root routes",
			routes: []Route{
				{
					Name:        "fallback",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/",
					UpstreamURL: mustParseURL(t, "http://fallback-service:8080"),
				},
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "https://users-service:8443"),
				},
				{
					Name:        "greeter",
					Protocol:    ProtocolGRPC,
					PathPrefix:  "/greeter.v1.Greeter",
					UpstreamURL: mustParseURL(t, "http://greeter-service:9090"),
				},
			},
		},
		{
			name:        "empty routes",
			wantMessage: "routes length must be greater than 0",
		},
		{
			name: "empty route name",
			routes: []Route{
				{
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "route name must not be empty",
		},
		{
			name: "whitespace route name",
			routes: []Route{
				{
					Name:        "   ",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "route name must not be empty",
		},
		{
			name: "unsupported protocol",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    Protocol("smtp"),
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: `unsupported protocol "smtp"`,
		},
		{
			name: "path prefix without leading slash",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "must start with '/'",
		},
		{
			name: "missing path matcher",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "either path prefix or exact path must be configured",
		},
		{
			name: "prefix and exact path together",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					PathExact:   "/users/42",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "path prefix and exact path are mutually exclusive",
		},
		{
			name: "whitespace path prefix is configured but invalid",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "   ",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "path prefix",
		},
		{
			name: "exact path without leading slash",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathExact:   "users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "exact path",
		},
		{
			name: "whitespace exact path is configured but invalid",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathExact:   "   ",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "exact path",
		},
		{
			name: "exact path cannot strip prefix",
			routes: []Route{
				{
					Name:            "users",
					Protocol:        ProtocolHTTP,
					PathExact:       "/users",
					StripPathPrefix: true,
					UpstreamURL:     mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "strip path prefix requires a path prefix matcher",
		},
		{
			name: "path prefix with trailing slash",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users/",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "must not end with '/'",
		},
		{
			name: "empty HTTP method",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{""},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "HTTP method must not be empty",
		},
		{
			name: "lowercase HTTP method",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"get"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: `HTTP method must be uppercase: "get"`,
		},
		{
			name: "invalid HTTP method token",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET /"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: `invalid HTTP method: "GET /"`,
		},
		{
			name: "duplicate HTTP method",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET", "GET"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: `duplicate HTTP method: "GET"`,
		},
		{
			name: "empty host",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{""},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host cannot be empty",
		},
		{
			name: "uppercase host",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"API.example.com"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host must be lowercase",
		},
		{
			name: "host with port",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api.example.com:8443"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "invalid host",
		},
		{
			name: "host with trailing dot",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api.example.com."},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host cannot have a trailing dot",
		},
		{
			name: "host with leading dot",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{".example.com"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host label cannot be empty",
		},
		{
			name: "host with empty label",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api..example.com"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host label cannot be empty",
		},
		{
			name: "host label starts with hyphen",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api.-internal.example"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host label cannot start or end with a hyphen",
		},
		{
			name: "host label ends with hyphen",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api-.internal.example"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host label cannot start or end with a hyphen",
		},
		{
			name: "host label exceeds maximum length",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{strings.Repeat("a", 64) + ".example"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host label cannot exceed 63 characters",
		},
		{
			name: "host exceeds maximum length",
			routes: []Route{
				{
					Name:       "users",
					Protocol:   ProtocolHTTP,
					PathPrefix: "/users",
					Hosts: []string{
						strings.Repeat("a", 63) + "." +
							strings.Repeat("b", 63) + "." +
							strings.Repeat("c", 63) + "." +
							strings.Repeat("d", 63),
					},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host cannot exceed 253 characters",
		},
		{
			name: "host contains invalid character",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api_internal.example"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: "host label contains invalid character",
		},
		{
			name: "duplicate host",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Hosts:       []string{"api.example.com", "api.example.com"},
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
			},
			wantMessage: `duplicate host: "api.example.com"`,
		},
		{
			name: "nil upstream URL",
			routes: []Route{
				{
					Name:       "users",
					Protocol:   ProtocolHTTP,
					PathPrefix: "/users",
				},
			},
			wantMessage: "upstream URL must not be nil",
		},
		{
			name: "unsupported upstream URL scheme",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "ftp://users-service:21"),
				},
			},
			wantMessage: `upstream URL scheme "ftp"`,
		},
		{
			name: "empty upstream URL host",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: &url.URL{Scheme: "http"},
				},
			},
			wantMessage: "upstream URL host must not be empty",
		},
		{
			name: "duplicate route name",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
				{
					Name:        "users",
					Protocol:    ProtocolGRPC,
					PathPrefix:  "/greeter.v1.Greeter",
					UpstreamURL: mustParseURL(t, "http://greeter-service:9090"),
				},
			},
			wantMessage: "duplicate route name",
		},
		{
			name: "same path prefix with disjoint methods",
			routes: []Route{
				{
					Name:        "get-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://users-read-service:8080"),
				},
				{
					Name:        "create-user",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"POST"},
					UpstreamURL: mustParseURL(t, "http://users-write-service:8080"),
				},
			},
		},
		{
			name: "same path and method with disjoint hosts",
			routes: []Route{
				{
					Name:        "public-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					Hosts:       []string{"api.example.com"},
					UpstreamURL: mustParseURL(t, "http://public-users-service:8080"),
				},
				{
					Name:        "internal-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					Hosts:       []string{"api.internal"},
					UpstreamURL: mustParseURL(t, "http://internal-users-service:8080"),
				},
			},
		},
		{
			name: "same path with overlapping methods and hosts",
			routes: []Route{
				{
					Name:        "users-v1",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET", "POST"},
					Hosts:       []string{"api.example.com", "api.internal"},
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:        "users-v2",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					Hosts:       []string{"api.internal"},
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
			},
			wantMessage: "conflicting matchers for path",
		},
		{
			name: "wildcard hosts conflict with constrained hosts for overlapping method",
			routes: []Route{
				{
					Name:        "all-hosts",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://all-hosts-service:8080"),
				},
				{
					Name:        "public-host",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					Hosts:       []string{"api.example.com"},
					UpstreamURL: mustParseURL(t, "http://public-host-service:8080"),
				},
			},
			wantMessage: "conflicting matchers for path",
		},
		{
			name: "same path prefix with overlapping methods",
			routes: []Route{
				{
					Name:        "read-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET", "HEAD"},
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:        "other-read-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
			},
			wantMessage: "conflicting matchers for path",
		},
		{
			name: "method conflicts with non-adjacent route at same path prefix",
			routes: []Route{
				{
					Name:        "first-get-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:        "post-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"POST"},
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
				{
					Name:        "second-get-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://users-v3-service:8080"),
				},
			},
			wantMessage: "conflicting matchers for path",
		},
		{
			name: "wildcard methods conflict with constrained methods",
			routes: []Route{
				{
					Name:        "all-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://all-users-service:8080"),
				},
				{
					Name:        "get-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://get-users-service:8080"),
				},
			},
			wantMessage: "conflicting matchers for path",
		},
		{
			name: "constrained methods conflict with wildcard methods",
			routes: []Route{
				{
					Name:        "get-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{"GET"},
					UpstreamURL: mustParseURL(t, "http://get-users-service:8080"),
				},
				{
					Name:        "all-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://all-users-service:8080"),
				},
			},
			wantMessage: "conflicting matchers for path",
		},
		{
			name: "same path method and host with disjoint header values",
			routes: []Route{
				{
					Name:          "production-users",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					Hosts:         []string{"api.example.com"},
					HeaderMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
					UpstreamURL:   mustParseURL(t, "http://production-users-service:8080"),
				},
				{
					Name:          "staging-users",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					Hosts:         []string{"api.example.com"},
					HeaderMatches: []HeaderMatch{{Name: "X-Environment", Exact: "staging"}},
					UpstreamURL:   mustParseURL(t, "http://staging-users-service:8080"),
				},
			},
		},
		{
			name: "identical header matchers conflict",
			routes: []Route{
				{
					Name:          "users-v1",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
					UpstreamURL:   mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:          "users-v2",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{{Name: "x-environment", Exact: "production"}},
					UpstreamURL:   mustParseURL(t, "http://users-v2-service:8080"),
				},
			},
			wantMessage: "conflicting matchers",
		},
		{
			name: "wildcard headers conflict with constrained headers",
			routes: []Route{
				{
					Name:        "all-environments",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Methods:     []string{http.MethodGet},
					UpstreamURL: mustParseURL(t, "http://all-users-service:8080"),
				},
				{
					Name:          "production-users",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
					UpstreamURL:   mustParseURL(t, "http://production-users-service:8080"),
				},
			},
			wantMessage: "conflicting matchers",
		},
		{
			name: "different header names still conflict",
			routes: []Route{
				{
					Name:          "production-users",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
					UpstreamURL:   mustParseURL(t, "http://production-users-service:8080"),
				},
				{
					Name:          "v2-users",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					Methods:       []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{{Name: "X-API-Version", Exact: "v2"}},
					UpstreamURL:   mustParseURL(t, "http://v2-users-service:8080"),
				},
			},
			wantMessage: "conflicting matchers",
		},
		{
			name: "later incompatible shared header makes routes disjoint",
			routes: []Route{
				{
					Name:       "users-v1",
					Protocol:   ProtocolHTTP,
					PathPrefix: "/users",
					Methods:    []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{
						{Name: "X-Environment", Exact: "production"},
						{Name: "X-API-Version", Exact: "v1"},
					},
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:       "users-v2",
					Protocol:   ProtocolHTTP,
					PathPrefix: "/users",
					Methods:    []string{http.MethodGet},
					HeaderMatches: []HeaderMatch{
						{Name: "X-Environment", Exact: "production"},
						{Name: "X-API-Version", Exact: "v2"},
					},
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := New(test.routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				if len(got.routes) != len(test.routes) {
					t.Errorf(
						"New() routes length = %d, want %d",
						len(got.routes),
						len(test.routes),
					)
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestNewValidatesExactPathConflicts(t *testing.T) {
	tests := []struct {
		name        string
		routes      []Route
		wantMessage string
	}{
		{
			name: "different exact paths are allowed",
			routes: []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
				},
				{
					Name:        "orders",
					Protocol:    ProtocolHTTP,
					PathExact:   "/orders",
					UpstreamURL: mustParseURL(t, "http://orders-service:8080"),
				},
			},
		},
		{
			name: "same exact path conflicts",
			routes: []Route{
				{
					Name:        "users-v1",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:        "users-v2",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
			},
			wantMessage: `conflicting matchers for path "/users"`,
		},
		{
			name: "same exact path with disjoint methods is allowed",
			routes: []Route{
				{
					Name:        "get-users",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					Methods:     []string{http.MethodGet},
					UpstreamURL: mustParseURL(t, "http://users-read-service:8080"),
				},
				{
					Name:        "create-user",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					Methods:     []string{http.MethodPost},
					UpstreamURL: mustParseURL(t, "http://users-write-service:8080"),
				},
			},
		},
		{
			name: "exact and prefix with same path are allowed",
			routes: []Route{
				{
					Name:        "prefix-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://prefix-users-service:8080"),
				},
				{
					Name:        "exact-users",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					UpstreamURL: mustParseURL(t, "http://exact-users-service:8080"),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := New(test.routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestNewValidatesRoutePriorities(t *testing.T) {
	tests := []struct {
		name        string
		priorities  [2]int
		wantMessage string
	}{
		{
			name:       "different positive and default priorities are allowed",
			priorities: [2]int{100, 0},
		},
		{
			name:       "negative and default priorities are allowed",
			priorities: [2]int{-10, 0},
		},
		{
			name:        "same positive priority conflicts",
			priorities:  [2]int{10, 10},
			wantMessage: `conflicting matchers for path "/users" at priority 10`,
		},
		{
			name:        "same negative priority conflicts",
			priorities:  [2]int{-10, -10},
			wantMessage: `conflicting matchers for path "/users" at priority -10`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:        "users-v1",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Priority:    test.priorities[0],
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:        "users-v2",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Priority:    test.priorities[1],
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestNewCopiesRoutes(t *testing.T) {
	upstreamURL := mustParseURL(t, "http://users-service:8080")
	routes := []Route{
		{
			Name:        "users",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/users",
			Methods:     []string{"GET"},
			UpstreamURL: upstreamURL,
		},
	}

	got, err := New(routes)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	routes[0] = Route{}

	want := Route{
		Name:        "users",
		Protocol:    ProtocolHTTP,
		PathPrefix:  "/users",
		Methods:     []string{"GET"},
		UpstreamURL: upstreamURL,
	}
	if !reflect.DeepEqual(got.routes[0], want) {
		t.Errorf("New() copied route = %+v, want %+v", got.routes[0], want)
	}
}

func TestNewValidatesHeaderMatches(t *testing.T) {
	tests := []struct {
		name          string
		headerMatches []HeaderMatch
		wantMessage   string
	}{
		{
			name: "valid exact matches",
			headerMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
		},
		{
			name:          "empty header name",
			headerMatches: []HeaderMatch{{Exact: "production"}},
			wantMessage:   "header name cannot be empty",
		},
		{
			name:          "invalid header name",
			headerMatches: []HeaderMatch{{Name: "X Environment", Exact: "production"}},
			wantMessage:   "invalid header name",
		},
		{
			name:          "empty exact value",
			headerMatches: []HeaderMatch{{Name: "X-Environment"}},
			wantMessage:   "header value cannot be empty",
		},
		{
			name:          "invalid exact value",
			headerMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production\n"}},
			wantMessage:   "invalid value for header",
		},
		{
			name:          "host header is reserved",
			headerMatches: []HeaderMatch{{Name: "Host", Exact: "api.example.com"}},
			wantMessage:   `header "Host" is reserved and cannot be matched`,
		},
		{
			name:          "forwarded header is reserved case insensitively",
			headerMatches: []HeaderMatch{{Name: "x-forwarded-for", Exact: "192.0.2.1"}},
			wantMessage:   `header "x-forwarded-for" is reserved and cannot be matched`,
		},
		{
			name: "case-insensitive duplicate header name",
			headerMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "x-environment", Exact: "staging"},
			},
			wantMessage: "duplicate header match",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:          "users",
					Protocol:      ProtocolHTTP,
					PathPrefix:    "/users",
					HeaderMatches: test.headerMatches,
					UpstreamURL:   mustParseURL(t, "http://users-service:8080"),
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			for _, context := range []string{"users", "header match policy", test.wantMessage} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("New() error = %q, want context %q", err, context)
				}
			}
		})
	}
}

func TestNewValidatesRateLimitPolicy(t *testing.T) {
	tests := []struct {
		name        string
		policy      RateLimitPolicy
		wantMessage string
	}{
		{
			name: "valid policy",
			policy: RateLimitPolicy{
				RequestsPerSecond: 10.5,
				Burst:             20,
			},
		},
		{
			name: "zero requests per second",
			policy: RateLimitPolicy{
				RequestsPerSecond: 0,
				Burst:             1,
			},
			wantMessage: "requests per second must be positive",
		},
		{
			name: "negative requests per second",
			policy: RateLimitPolicy{
				RequestsPerSecond: -1,
				Burst:             1,
			},
			wantMessage: "requests per second must be positive",
		},
		{
			name: "NaN requests per second",
			policy: RateLimitPolicy{
				RequestsPerSecond: math.NaN(),
				Burst:             1,
			},
			wantMessage: "requests per second must be finite",
		},
		{
			name: "positive infinity requests per second",
			policy: RateLimitPolicy{
				RequestsPerSecond: math.Inf(1),
				Burst:             1,
			},
			wantMessage: "requests per second must be finite",
		},
		{
			name: "negative infinity requests per second",
			policy: RateLimitPolicy{
				RequestsPerSecond: math.Inf(-1),
				Burst:             1,
			},
			wantMessage: "requests per second must be finite",
		},
		{
			name: "zero burst",
			policy: RateLimitPolicy{
				RequestsPerSecond: 1,
				Burst:             0,
			},
			wantMessage: "burst must be positive",
		},
		{
			name: "negative burst",
			policy: RateLimitPolicy{
				RequestsPerSecond: 1,
				Burst:             -1,
			},
			wantMessage: "burst must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:        "users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-service:8080"),
					RateLimit:   &test.policy,
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			for _, context := range []string{"users", "rate limit policy", test.wantMessage} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("New() error = %q, want context %q", err, context)
				}
			}
		})
	}
}

func TestNewValidatesRequestTimeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		wantMessage string
	}{
		{
			name:    "zero disables timeout",
			timeout: 0,
		},
		{
			name:    "positive timeout",
			timeout: 2500 * time.Millisecond,
		},
		{
			name:        "negative timeout",
			timeout:     -time.Nanosecond,
			wantMessage: "request timeout must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:           "users",
					Protocol:       ProtocolHTTP,
					PathPrefix:     "/users",
					UpstreamURL:    mustParseURL(t, "http://users-service:8080"),
					RequestTimeout: test.timeout,
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			for _, context := range []string{"users", test.wantMessage} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("New() error = %q, want context %q", err, context)
				}
			}
		})
	}
}

func TestNewValidatesMaxRequestBodyBytes(t *testing.T) {
	tests := []struct {
		name        string
		limit       int64
		wantMessage string
	}{
		{
			name: "zero disables limit",
		},
		{
			name:  "positive limit",
			limit: 10 * 1024 * 1024,
		},
		{
			name:        "negative limit",
			limit:       -1,
			wantMessage: "max request body bytes must not be negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:                "users",
					Protocol:            ProtocolHTTP,
					PathPrefix:          "/users",
					UpstreamURL:         mustParseURL(t, "http://users-service:8080"),
					MaxRequestBodyBytes: test.limit,
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			for _, context := range []string{"users", test.wantMessage} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("New() error = %q, want context %q", err, context)
				}
			}
		})
	}
}

func TestNewValidatesRequestHeaderPolicy(t *testing.T) {
	tests := []struct {
		name             string
		policy           *HeaderTransformPolicy
		wantMessage      string
		forbiddenMessage string
	}{
		{
			name:   "empty policy",
			policy: &HeaderTransformPolicy{},
		},
		{
			name: "valid set and remove",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{
					"X-Gateway":     "DevGate",
					"X-Empty-Value": "",
				},
				Remove: []string{"X-Legacy-Header"},
			},
		},
		{
			name: "invalid set name",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{"Bad Header": "value"},
			},
			wantMessage: "invalid header name to set",
		},
		{
			name: "invalid set value",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{"X-Gateway": "safe\r\nInjected: true"},
			},
			wantMessage:      `invalid value for header "X-Gateway"`,
			forbiddenMessage: "safe\r\nInjected: true",
		},
		{
			name: "invalid remove name",
			policy: &HeaderTransformPolicy{
				Remove: []string{"Bad Header"},
			},
			wantMessage: "invalid header name to remove",
		},
		{
			name: "reserved set header",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{"hOsT": "upstream.example"},
			},
			wantMessage: `header "hOsT" is reserved`,
		},
		{
			name: "reserved remove header",
			policy: &HeaderTransformPolicy{
				Remove: []string{"X-fOrWaRdEd-Custom"},
			},
			wantMessage: `header "X-fOrWaRdEd-Custom" is reserved`,
		},
		{
			name: "duplicate set header",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{
					"X-Environment": "production",
					"x-environment": "staging",
				},
			},
			wantMessage: `duplicate header name to set: "x-environment"`,
		},
		{
			name: "duplicate remove header",
			policy: &HeaderTransformPolicy{
				Remove: []string{"X-Legacy", "x-legacy"},
			},
			wantMessage: `duplicate header name to remove: "x-legacy"`,
		},
		{
			name: "header set and removed",
			policy: &HeaderTransformPolicy{
				Set:    map[string]string{"X-Environment": "production"},
				Remove: []string{"x-environment"},
			},
			wantMessage: `header "x-environment" cannot be both set and removed`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:           "users",
					Protocol:       ProtocolHTTP,
					PathPrefix:     "/users",
					UpstreamURL:    mustParseURL(t, "http://users-service:8080"),
					RequestHeaders: test.policy,
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			for _, context := range []string{"users", "request headers policy", test.wantMessage} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("New() error = %q, want context %q", err, context)
				}
			}
			if test.forbiddenMessage != "" && strings.Contains(err.Error(), test.forbiddenMessage) {
				t.Errorf("New() error = %q, must not contain sensitive header value", err)
			}
		})
	}
}

func TestIsReservedRequestHeader(t *testing.T) {
	reserved := []string{
		"Host",
		"Connection",
		"Content-Length",
		"Forwarded",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
		"X-Real-IP",
		"X-Request-ID",
		"x-FoRwArDeD-For",
		"X-Forwarded-Custom",
	}

	for _, name := range reserved {
		t.Run("reserved "+name, func(t *testing.T) {
			if !isReservedRequestHeader(name) {
				t.Errorf("isReservedRequestHeader(%q) = false, want true", name)
			}
		})
	}

	allowed := []string{
		"Authorization",
		"Cookie",
		"X-Gateway",
		"X-Forwardedness",
		"X-ForwardedCustom",
	}

	for _, name := range allowed {
		t.Run("allowed "+name, func(t *testing.T) {
			if isReservedRequestHeader(name) {
				t.Errorf("isReservedRequestHeader(%q) = true, want false", name)
			}
		})
	}
}

func TestNewValidatesResponseHeaderPolicy(t *testing.T) {
	tests := []struct {
		name             string
		policy           *HeaderTransformPolicy
		wantMessage      string
		forbiddenMessage string
	}{
		{
			name:   "empty policy",
			policy: &HeaderTransformPolicy{},
		},
		{
			name: "valid set and remove",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{
					"X-Content-Type-Options": "nosniff",
					"X-Empty-Value":          "",
				},
				Remove: []string{"Server", "X-Real-IP"},
			},
		},
		{
			name: "invalid set name",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{"Bad Header": "value"},
			},
			wantMessage: "invalid header name to set",
		},
		{
			name: "invalid set value",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{"X-Gateway": "safe\r\nInjected: true"},
			},
			wantMessage:      `invalid value for header "X-Gateway"`,
			forbiddenMessage: "safe\r\nInjected: true",
		},
		{
			name: "invalid remove name",
			policy: &HeaderTransformPolicy{
				Remove: []string{"Bad Header"},
			},
			wantMessage: "invalid header name to remove",
		},
		{
			name: "reserved set header",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{"cOnTeNt-EnCoDiNg": "gzip"},
			},
			wantMessage: `header "cOnTeNt-EnCoDiNg" is reserved`,
		},
		{
			name: "reserved remove header",
			policy: &HeaderTransformPolicy{
				Remove: []string{"X-rEqUeSt-Id"},
			},
			wantMessage: `header "X-rEqUeSt-Id" is reserved`,
		},
		{
			name: "duplicate set header",
			policy: &HeaderTransformPolicy{
				Set: map[string]string{
					"X-Environment": "production",
					"x-environment": "staging",
				},
			},
			wantMessage: `duplicate header name to set: "x-environment"`,
		},
		{
			name: "duplicate remove header",
			policy: &HeaderTransformPolicy{
				Remove: []string{"Server", "server"},
			},
			wantMessage: `duplicate header name to remove: "server"`,
		},
		{
			name: "header set and removed",
			policy: &HeaderTransformPolicy{
				Set:    map[string]string{"X-Environment": "production"},
				Remove: []string{"x-environment"},
			},
			wantMessage: `header "x-environment" cannot be both set and removed`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := []Route{
				{
					Name:            "users",
					Protocol:        ProtocolHTTP,
					PathPrefix:      "/users",
					UpstreamURL:     mustParseURL(t, "http://users-service:8080"),
					ResponseHeaders: test.policy,
				},
			}

			got, err := New(routes)
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			for _, context := range []string{"users", "response headers policy", test.wantMessage} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("New() error = %q, want context %q", err, context)
				}
			}
			if test.forbiddenMessage != "" && strings.Contains(err.Error(), test.forbiddenMessage) {
				t.Errorf("New() error = %q, must not contain sensitive header value", err)
			}
		})
	}
}

func TestIsReservedResponseHeader(t *testing.T) {
	reserved := []string{
		"Connection",
		"Content-Length",
		"Content-Encoding",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
		"x-ReQuEsT-iD",
	}

	for _, name := range reserved {
		t.Run("reserved "+name, func(t *testing.T) {
			if !isReservedResponseHeader(name) {
				t.Errorf("isReservedResponseHeader(%q) = false, want true", name)
			}
		})
	}

	allowed := []string{
		"Content-Security-Policy",
		"Content-Type",
		"Server",
		"Set-Cookie",
		"X-Forwarded-For",
		"X-Real-IP",
	}

	for _, name := range allowed {
		t.Run("allowed "+name, func(t *testing.T) {
			if isReservedResponseHeader(name) {
				t.Errorf("isReservedResponseHeader(%q) = true, want false", name)
			}
		})
	}
}

func TestHeaderTransformPolicyApply(t *testing.T) {
	header := make(http.Header)
	header.Add("X-Replace", "first")
	header.Add("X-Replace", "second")
	header.Set("X-Remove", "remove me")
	header.Set("X-Keep", "keep me")

	policy := HeaderTransformPolicy{
		Set: map[string]string{
			"x-replace": "replacement",
			"X-Added":   "added",
			"X-Empty":   "",
		},
		Remove: []string{"x-remove"},
	}

	policy.Apply(header)

	tests := []struct {
		name       string
		headerName string
		wantValues []string
	}{
		{
			name:       "replace all existing values",
			headerName: "X-Replace",
			wantValues: []string{"replacement"},
		},
		{
			name:       "add header",
			headerName: "X-Added",
			wantValues: []string{"added"},
		},
		{
			name:       "preserve empty value",
			headerName: "X-Empty",
			wantValues: []string{""},
		},
		{
			name:       "remove header case insensitively",
			headerName: "X-Remove",
		},
		{
			name:       "preserve unrelated header",
			headerName: "X-Keep",
			wantValues: []string{"keep me"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := header.Values(test.headerName); !slices.Equal(got, test.wantValues) {
				t.Errorf("header.Values(%q) = %q, want %q", test.headerName, got, test.wantValues)
			}
		})
	}
}

func TestRouterMatch(t *testing.T) {
	routes := []Route{
		{
			Name:        "fallback",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/",
			UpstreamURL: mustParseURL(t, "http://fallback-service:8080"),
		},
		{
			Name:        "admin-write",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api/admin",
			Methods:     []string{"POST"},
			UpstreamURL: mustParseURL(t, "http://admin-service:8080"),
		},
		{
			Name:        "api-read",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"GET"},
			UpstreamURL: mustParseURL(t, "http://api-read-service:8080"),
		},
		{
			Name:        "api-write",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"POST"},
			UpstreamURL: mustParseURL(t, "http://api-write-service:8080"),
		},
		{
			Name:        "greeter",
			Protocol:    ProtocolGRPC,
			PathPrefix:  "/greeter.v1.Greeter",
			Methods:     []string{"POST"},
			UpstreamURL: mustParseURL(t, "http://greeter-service:9090"),
		},
	}

	router, err := New(routes)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name          string
		method        string
		path          string
		wantRouteName string
	}{
		{
			name:          "root path uses fallback",
			method:        http.MethodGet,
			path:          "/",
			wantRouteName: "fallback",
		},
		{
			name:          "GET uses read route",
			method:        http.MethodGet,
			path:          "/api",
			wantRouteName: "api-read",
		},
		{
			name:          "POST uses write route at same prefix",
			method:        http.MethodPost,
			path:          "/api",
			wantRouteName: "api-write",
		},
		{
			name:          "API prefix with trailing slash",
			method:        http.MethodGet,
			path:          "/api/",
			wantRouteName: "api-read",
		},
		{
			name:          "API child path",
			method:        http.MethodGet,
			path:          "/api/users",
			wantRouteName: "api-read",
		},
		{
			name:          "exact admin prefix uses longer method-compatible match",
			method:        http.MethodPost,
			path:          "/api/admin",
			wantRouteName: "admin-write",
		},
		{
			name:          "admin child path uses longer method-compatible match",
			method:        http.MethodPost,
			path:          "/api/admin/users",
			wantRouteName: "admin-write",
		},
		{
			name:          "method mismatch on longer prefix falls back to shorter route",
			method:        http.MethodGet,
			path:          "/api/admin/users",
			wantRouteName: "api-read",
		},
		{
			name:          "method mismatch on constrained routes uses wildcard fallback",
			method:        http.MethodDelete,
			path:          "/api/admin/users",
			wantRouteName: "fallback",
		},
		{
			name:          "similar segment does not match API",
			method:        http.MethodGet,
			path:          "/apix",
			wantRouteName: "fallback",
		},
		{
			name:          "gRPC method path",
			method:        http.MethodPost,
			path:          "/greeter.v1.Greeter/SayHello",
			wantRouteName: "greeter",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := router.Match(test.method, "", test.path, nil)
			if !found {
				t.Fatalf("Match(%q, %q) found = false, want true", test.method, test.path)
			}
			if got.Name != test.wantRouteName {
				t.Errorf(
					"Match(%q, %q) route name = %q, want %q",
					test.method,
					test.path,
					got.Name,
					test.wantRouteName,
				)
			}
		})
	}
}

func TestRouterMatchNotFound(t *testing.T) {
	router, err := New([]Route{
		{
			Name:        "api",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"GET"},
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "unrelated path",
			method: http.MethodGet,
			path:   "/orders",
		},
		{
			name:   "similar segment",
			method: http.MethodGet,
			path:   "/apix",
		},
		{
			name:   "case-sensitive path",
			method: http.MethodGet,
			path:   "/API",
		},
		{
			name:   "relative path",
			method: http.MethodGet,
			path:   "api/users",
		},
		{
			name:   "method mismatch",
			method: http.MethodPost,
			path:   "/api/users",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := router.Match(test.method, "", test.path, nil)
			if found {
				t.Errorf("Match(%q, %q) found = true, want false", test.method, test.path)
			}
			if !reflect.DeepEqual(got, Route{}) {
				t.Errorf("Match(%q, %q) route = %+v, want zero Route", test.method, test.path, got)
			}
		})
	}
}

func TestRouterMatchHost(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:        "fallback",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/",
			UpstreamURL: mustParseURL(t, "http://fallback-service:8080"),
		},
		{
			Name:        "public-users",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/users",
			Methods:     []string{"GET"},
			Hosts:       []string{"api.example.com"},
			UpstreamURL: mustParseURL(t, "http://public-users-service:8080"),
		},
		{
			Name:        "internal-users",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/users",
			Methods:     []string{"GET"},
			Hosts:       []string{"api.internal"},
			UpstreamURL: mustParseURL(t, "http://internal-users-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name          string
		host          string
		wantRouteName string
	}{
		{
			name:          "exact public host",
			host:          "api.example.com",
			wantRouteName: "public-users",
		},
		{
			name:          "host is case insensitive and ignores port",
			host:          "API.EXAMPLE.COM:8443",
			wantRouteName: "public-users",
		},
		{
			name:          "exact internal host",
			host:          "api.internal",
			wantRouteName: "internal-users",
		},
		{
			name:          "unknown host uses wildcard fallback",
			host:          "attacker.example",
			wantRouteName: "fallback",
		},
		{
			name:          "similar host uses wildcard fallback",
			host:          "notapi.example.com",
			wantRouteName: "fallback",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := routeRouter.Match(http.MethodGet, test.host, "/users/42", nil)
			if !found {
				t.Fatalf("Match(%q, %q, %q) found = false, want true", http.MethodGet, test.host, "/users/42")
			}
			if got.Name != test.wantRouteName {
				t.Errorf(
					"Match(%q, %q, %q) route name = %q, want %q",
					http.MethodGet,
					test.host,
					"/users/42",
					got.Name,
					test.wantRouteName,
				)
			}
		})
	}
}

func TestRouterMatchPrefersExactPathOverEqualPrefix(t *testing.T) {
	exactRoute := Route{
		Name:        "exact-users",
		Protocol:    ProtocolHTTP,
		PathExact:   "/users",
		UpstreamURL: mustParseURL(t, "http://exact-users-service:8080"),
	}
	prefixRoute := Route{
		Name:        "prefix-users",
		Protocol:    ProtocolHTTP,
		PathPrefix:  "/users",
		UpstreamURL: mustParseURL(t, "http://prefix-users-service:8080"),
	}

	tests := []struct {
		name   string
		routes []Route
	}{
		{
			name:   "prefix before exact",
			routes: []Route{prefixRoute, exactRoute},
		},
		{
			name:   "exact before prefix",
			routes: []Route{exactRoute, prefixRoute},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routeRouter, err := New(test.routes)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			exactMatch, found := routeRouter.Match(http.MethodGet, "", "/users", nil)
			if !found {
				t.Fatal("Match() exact path found = false, want true")
			}
			if exactMatch.Name != "exact-users" {
				t.Errorf("Match() exact path route = %q, want %q", exactMatch.Name, "exact-users")
			}

			prefixMatch, found := routeRouter.Match(http.MethodGet, "", "/users/42", nil)
			if !found {
				t.Fatal("Match() child path found = false, want true")
			}
			if prefixMatch.Name != "prefix-users" {
				t.Errorf("Match() child path route = %q, want %q", prefixMatch.Name, "prefix-users")
			}
		})
	}
}

func TestRouterMatchUsesPriorityAfterPathSpecificity(t *testing.T) {
	lowPriorityRoute := Route{
		Name:        "low-priority",
		Protocol:    ProtocolHTTP,
		PathPrefix:  "/users",
		Priority:    0,
		UpstreamURL: mustParseURL(t, "http://low-priority-service:8080"),
	}
	highPriorityRoute := Route{
		Name:        "high-priority",
		Protocol:    ProtocolHTTP,
		PathPrefix:  "/users",
		Priority:    100,
		UpstreamURL: mustParseURL(t, "http://high-priority-service:8080"),
	}
	headerRoute := Route{
		Name:       "production-users",
		Protocol:   ProtocolHTTP,
		PathPrefix: "/users",
		HeaderMatches: []HeaderMatch{
			{Name: "X-Environment", Exact: "production"},
		},
		Priority:    100,
		UpstreamURL: mustParseURL(t, "http://production-users-service:8080"),
	}

	tests := []struct {
		name          string
		routes        []Route
		path          string
		headers       http.Header
		wantRouteName string
	}{
		{
			name:          "higher priority wins when registered last",
			routes:        []Route{lowPriorityRoute, highPriorityRoute},
			path:          "/users/42",
			wantRouteName: "high-priority",
		},
		{
			name:          "higher priority wins when registered first",
			routes:        []Route{highPriorityRoute, lowPriorityRoute},
			path:          "/users/42",
			wantRouteName: "high-priority",
		},
		{
			name: "default priority wins over negative priority",
			routes: []Route{
				lowPriorityRoute,
				{
					Name:        "negative-priority",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Priority:    -10,
					UpstreamURL: mustParseURL(t, "http://negative-priority-service:8080"),
				},
			},
			path:          "/users/42",
			wantRouteName: "low-priority",
		},
		{
			name: "longer path wins over higher priority",
			routes: []Route{
				{
					Name:        "specific-users",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/api/users",
					Priority:    -100,
					UpstreamURL: mustParseURL(t, "http://specific-users-service:8080"),
				},
				{
					Name:        "high-priority-api",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/api",
					Priority:    1_000,
					UpstreamURL: mustParseURL(t, "http://high-priority-api-service:8080"),
				},
			},
			path:          "/api/users/42",
			wantRouteName: "specific-users",
		},
		{
			name: "exact path wins over higher priority prefix",
			routes: []Route{
				{
					Name:        "exact-users",
					Protocol:    ProtocolHTTP,
					PathExact:   "/users",
					Priority:    -100,
					UpstreamURL: mustParseURL(t, "http://exact-users-service:8080"),
				},
				{
					Name:        "high-priority-prefix",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					Priority:    1_000,
					UpstreamURL: mustParseURL(t, "http://high-priority-prefix-service:8080"),
				},
			},
			path:          "/users",
			wantRouteName: "exact-users",
		},
		{
			name:          "matching header route wins by priority",
			routes:        []Route{lowPriorityRoute, headerRoute},
			path:          "/users/42",
			headers:       http.Header{"X-Environment": []string{"production"}},
			wantRouteName: "production-users",
		},
		{
			name:          "wildcard route handles unmatched header",
			routes:        []Route{lowPriorityRoute, headerRoute},
			path:          "/users/42",
			headers:       http.Header{"X-Environment": []string{"staging"}},
			wantRouteName: "low-priority",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routeRouter, err := New(test.routes)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			got, found := routeRouter.Match(http.MethodGet, "", test.path, test.headers)
			if !found {
				t.Fatal("Match() found = false, want true")
			}
			if got.Name != test.wantRouteName {
				t.Errorf("Match() route = %q, want %q", got.Name, test.wantRouteName)
			}
		})
	}
}

func TestRouterMatchExactPathPreservesTrailingSlash(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:        "fallback",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/",
			UpstreamURL: mustParseURL(t, "http://fallback-service:8080"),
		},
		{
			Name:        "exact-users",
			Protocol:    ProtocolHTTP,
			PathExact:   "/users/",
			UpstreamURL: mustParseURL(t, "http://exact-users-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name          string
		path          string
		wantRouteName string
	}{
		{
			name:          "trailing slash matches exact route",
			path:          "/users/",
			wantRouteName: "exact-users",
		},
		{
			name:          "missing trailing slash uses fallback",
			path:          "/users",
			wantRouteName: "fallback",
		},
		{
			name:          "child path uses fallback",
			path:          "/users/42",
			wantRouteName: "fallback",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := routeRouter.Match(http.MethodGet, "", test.path, nil)
			if !found {
				t.Fatal("Match() found = false, want true")
			}
			if got.Name != test.wantRouteName {
				t.Errorf("Match() route = %q, want %q", got.Name, test.wantRouteName)
			}
		})
	}
}

func TestRouterMatchHeaders(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:        "fallback",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/",
			UpstreamURL: mustParseURL(t, "http://fallback-service:8080"),
		},
		{
			Name:       "production-users",
			Protocol:   ProtocolHTTP,
			PathPrefix: "/users",
			HeaderMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
			},
			UpstreamURL: mustParseURL(t, "http://production-users-service:8080"),
		},
		{
			Name:       "staging-users",
			Protocol:   ProtocolHTTP,
			PathPrefix: "/users",
			HeaderMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "staging"},
			},
			UpstreamURL: mustParseURL(t, "http://staging-users-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name          string
		headers       http.Header
		wantRouteName string
	}{
		{
			name:          "matching header selects constrained route",
			headers:       http.Header{"X-Environment": []string{"production"}},
			wantRouteName: "production-users",
		},
		{
			name:          "missing header uses fallback",
			wantRouteName: "fallback",
		},
		{
			name:          "different exact value selects sibling route",
			headers:       http.Header{"X-Environment": []string{"staging"}},
			wantRouteName: "staging-users",
		},
		{
			name:          "unknown value uses fallback",
			headers:       http.Header{"X-Environment": []string{"development"}},
			wantRouteName: "fallback",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := routeRouter.Match(http.MethodGet, "", "/users/42", test.headers)
			if !found {
				t.Fatal("Match() found = false, want true")
			}
			if got.Name != test.wantRouteName {
				t.Errorf("Match() route name = %q, want %q", got.Name, test.wantRouteName)
			}
		})
	}
}

func TestRouterAllowedMethods(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:        "api-read",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"GET"},
			UpstreamURL: mustParseURL(t, "http://api-read-service:8080"),
		},
		{
			Name:        "admin-write",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api/admin",
			Methods:     []string{"POST"},
			UpstreamURL: mustParseURL(t, "http://admin-write-service:8080"),
		},
		{
			Name:        "audit-read",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api/admin/audit",
			Methods:     []string{"GET"},
			UpstreamURL: mustParseURL(t, "http://audit-read-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "collects methods from all matching prefixes",
			path: "/api/admin/audit/events",
			want: []string{"GET", "POST"},
		},
		{
			name: "matches complete path segments",
			path: "/apix",
		},
		{
			name: "unmatched path",
			path: "/orders",
		},
		{
			name: "relative path",
			path: "api/admin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := routeRouter.AllowedMethods("", test.path, nil)
			if !slices.Equal(got, test.want) {
				t.Errorf("AllowedMethods(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestRouterAllowedMethodsReturnsNilForWildcardRoute(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:        "fallback",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/",
			UpstreamURL: mustParseURL(t, "http://fallback-service:8080"),
		},
		{
			Name:        "api-read",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"GET"},
			UpstreamURL: mustParseURL(t, "http://api-read-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := routeRouter.AllowedMethods("", "/api/users", nil); got != nil {
		t.Errorf("AllowedMethods(%q) = %q, want nil for wildcard route", "/api/users", got)
	}
}

func TestRouterAllowedMethodsFiltersByHost(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:        "public-api-read",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"GET"},
			Hosts:       []string{"api.example.com"},
			UpstreamURL: mustParseURL(t, "http://public-api-service:8080"),
		},
		{
			Name:        "public-admin-write",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api/admin",
			Methods:     []string{"POST"},
			Hosts:       []string{"api.example.com"},
			UpstreamURL: mustParseURL(t, "http://public-admin-service:8080"),
		},
		{
			Name:        "internal-api",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			Methods:     []string{"DELETE"},
			Hosts:       []string{"api.internal"},
			UpstreamURL: mustParseURL(t, "http://internal-api-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		host string
		want []string
	}{
		{
			name: "public host with port",
			host: "API.EXAMPLE.COM:443",
			want: []string{"GET", "POST"},
		},
		{
			name: "internal host",
			host: "api.internal",
			want: []string{"DELETE"},
		},
		{
			name: "unknown host",
			host: "unknown.example",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := routeRouter.AllowedMethods(test.host, "/api/admin/users", nil)
			if !slices.Equal(got, test.want) {
				t.Errorf("AllowedMethods(%q, %q) = %q, want %q", test.host, "/api/admin/users", got, test.want)
			}
		})
	}
}

func TestRouterAllowedMethodsFiltersByHeaders(t *testing.T) {
	routeRouter, err := New([]Route{
		{
			Name:       "production-users",
			Protocol:   ProtocolHTTP,
			PathPrefix: "/users",
			Methods:    []string{http.MethodGet},
			HeaderMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
			},
			UpstreamURL: mustParseURL(t, "http://production-users-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name    string
		headers http.Header
		want    []string
	}{
		{
			name:    "matching header returns allowed method",
			headers: http.Header{"X-Environment": []string{"production"}},
			want:    []string{http.MethodGet},
		},
		{
			name: "missing header returns no methods",
		},
		{
			name:    "different header value returns no methods",
			headers: http.Header{"X-Environment": []string{"staging"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := routeRouter.AllowedMethods("", "/users/42", test.headers)
			if !slices.Equal(got, test.want) {
				t.Errorf("AllowedMethods() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRouteMatchesHeaders(t *testing.T) {
	tests := []struct {
		name          string
		headerMatches []HeaderMatch
		headers       http.Header
		want          bool
	}{
		{
			name: "route without header constraints",
			want: true,
		},
		{
			name:          "exact match",
			headerMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			headers:       http.Header{"X-Environment": []string{"production"}},
			want:          true,
		},
		{
			name:          "header name is case insensitive",
			headerMatches: []HeaderMatch{{Name: "x-environment", Exact: "production"}},
			headers:       http.Header{"X-Environment": []string{"production"}},
			want:          true,
		},
		{
			name:          "header value is case sensitive",
			headerMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			headers:       http.Header{"X-Environment": []string{"Production"}},
		},
		{
			name:          "missing header",
			headerMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
		},
		{
			name: "all conditions match",
			headerMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
			headers: http.Header{
				"X-Environment": []string{"production"},
				"X-Api-Version": []string{"v2"},
			},
			want: true,
		},
		{
			name: "one condition does not match",
			headerMatches: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
			headers: http.Header{
				"X-Environment": []string{"production"},
				"X-Api-Version": []string{"v1"},
			},
		},
		{
			name:          "multiple values are ambiguous",
			headerMatches: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			headers:       http.Header{"X-Environment": []string{"production", "staging"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := Route{HeaderMatches: test.headerMatches}

			got := routeMatchesHeaders(route, test.headers)

			if got != test.want {
				t.Errorf("routeMatchesHeaders() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestHeaderMatchSetsOverlap(t *testing.T) {
	tests := []struct {
		name     string
		current  []HeaderMatch
		existing []HeaderMatch
		want     bool
	}{
		{
			name: "unconstrained routes overlap",
			want: true,
		},
		{
			name:     "wildcard current overlaps constrained existing",
			existing: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			want:     true,
		},
		{
			name:    "constrained current overlaps wildcard existing",
			current: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			want:    true,
		},
		{
			name:     "identical conditions overlap",
			current:  []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			existing: []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			want:     true,
		},
		{
			name:     "different header names can coexist",
			current:  []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			existing: []HeaderMatch{{Name: "X-API-Version", Exact: "v2"}},
			want:     true,
		},
		{
			name:     "different values for same header are disjoint",
			current:  []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			existing: []HeaderMatch{{Name: "X-Environment", Exact: "staging"}},
		},
		{
			name: "one shared condition and one independent condition overlap",
			current: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
			},
			existing: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
			want: true,
		},
		{
			name: "later conflicting condition makes sets disjoint",
			current: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v1"},
			},
			existing: []HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
		},
		{
			name:     "header names are case insensitive",
			current:  []HeaderMatch{{Name: "X-Environment", Exact: "production"}},
			existing: []HeaderMatch{{Name: "x-environment", Exact: "staging"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := headerMatchSetsOverlap(test.current, test.existing)
			if got != test.want {
				t.Errorf("headerMatchSetsOverlap() = %t, want %t", got, test.want)
			}
		})
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}

	return parsedURL
}
