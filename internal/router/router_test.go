package router

import (
	"math"
	"net/http"
	"net/url"
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
			name: "duplicate route path prefix",
			routes: []Route{
				{
					Name:        "users-v1",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-v1-service:8080"),
				},
				{
					Name:        "users-v2",
					Protocol:    ProtocolHTTP,
					PathPrefix:  "/users",
					UpstreamURL: mustParseURL(t, "http://users-v2-service:8080"),
				},
			},
			wantMessage: "duplicate route path prefix",
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

func TestNewCopiesRoutes(t *testing.T) {
	upstreamURL := mustParseURL(t, "http://users-service:8080")
	routes := []Route{
		{
			Name:        "users",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/users",
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
		UpstreamURL: upstreamURL,
	}
	if got.routes[0] != want {
		t.Errorf("New() copied route = %+v, want %+v", got.routes[0], want)
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
			Name:        "admin",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api/admin",
			UpstreamURL: mustParseURL(t, "http://admin-service:8080"),
		},
		{
			Name:        "api",
			Protocol:    ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
		{
			Name:        "greeter",
			Protocol:    ProtocolGRPC,
			PathPrefix:  "/greeter.v1.Greeter",
			UpstreamURL: mustParseURL(t, "http://greeter-service:9090"),
		},
	}

	router, err := New(routes)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name          string
		path          string
		wantRouteName string
	}{
		{
			name:          "root path uses fallback",
			path:          "/",
			wantRouteName: "fallback",
		},
		{
			name:          "exact API prefix",
			path:          "/api",
			wantRouteName: "api",
		},
		{
			name:          "API prefix with trailing slash",
			path:          "/api/",
			wantRouteName: "api",
		},
		{
			name:          "API child path",
			path:          "/api/users",
			wantRouteName: "api",
		},
		{
			name:          "exact admin prefix uses longer match",
			path:          "/api/admin",
			wantRouteName: "admin",
		},
		{
			name:          "admin child path uses longer match",
			path:          "/api/admin/users",
			wantRouteName: "admin",
		},
		{
			name:          "similar segment does not match API",
			path:          "/apix",
			wantRouteName: "fallback",
		},
		{
			name:          "gRPC method path",
			path:          "/greeter.v1.Greeter/SayHello",
			wantRouteName: "greeter",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := router.Match(test.path)
			if !found {
				t.Fatalf("Match(%q) found = false, want true", test.path)
			}
			if got.Name != test.wantRouteName {
				t.Errorf(
					"Match(%q) route name = %q, want %q",
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
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{
			name: "unrelated path",
			path: "/orders",
		},
		{
			name: "similar segment",
			path: "/apix",
		},
		{
			name: "case-sensitive path",
			path: "/API",
		},
		{
			name: "relative path",
			path: "api/users",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, found := router.Match(test.path)
			if found {
				t.Errorf("Match(%q) found = true, want false", test.path)
			}
			if got != (Route{}) {
				t.Errorf("Match(%q) route = %+v, want zero Route", test.path, got)
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
