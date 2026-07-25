package router

import (
	"net/url"
	"strings"
	"testing"
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

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}

	return parsedURL
}
