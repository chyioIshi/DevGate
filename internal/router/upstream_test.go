package router

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestUpstreamValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		upstream    Upstream
		wantErr     bool
		errContains []string
	}{
		{
			name: "valid HTTP and HTTPS endpoints",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyRoundRobin,
				Endpoints: []url.URL{
					{Scheme: "http", Host: "server-1:8080"},
					{Scheme: "https", Host: "server-2:8443"},
				},
			},
		},
		{
			name: "valid random policy",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyRandom,
				Endpoints: []url.URL{
					{Scheme: "http", Host: "server-1:8080"},
					{Scheme: "http", Host: "server-2:8080"},
				},
			},
		},
		{
			name: "valid least requests policy",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyLeastRequests,
				Endpoints: []url.URL{
					{Scheme: "http", Host: "server-1:8080"},
					{Scheme: "http", Host: "server-2:8080"},
				},
			},
		},
		{
			name: "unsupported load balancing policy",
			upstream: Upstream{
				LoadBalancing: "least_connections",
				Endpoints: []url.URL{
					{Scheme: "http", Host: "server-1:8080"},
				},
			},
			wantErr:     true,
			errContains: []string{"load balancing policy", "least_connections"},
		},
		{
			name: "no endpoints",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyRoundRobin,
			},
			wantErr:     true,
			errContains: []string{"at least one upstream endpoint"},
		},
		{
			name: "unsupported endpoint scheme",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyRoundRobin,
				Endpoints: []url.URL{
					{Scheme: "http", Host: "server-1:8080"},
					{Scheme: "ftp", Host: "server-2:8080"},
				},
			},
			wantErr:     true,
			errContains: []string{"1", "scheme"},
		},
		{
			name: "empty endpoint host",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyRoundRobin,
				Endpoints: []url.URL{
					{Scheme: "http", Host: ""},
				},
			},
			wantErr:     true,
			errContains: []string{"0", "host"},
		},
		{
			name: "blank endpoint host",
			upstream: Upstream{
				LoadBalancing: LoadBalancingPolicyRoundRobin,
				Endpoints: []url.URL{
					{Scheme: "http", Host: "   "},
				},
			},
			wantErr:     true,
			errContains: []string{"0", "host"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.upstream.validate()
			if tt.wantErr && err == nil {
				t.Fatal("validate() error = nil, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validate() error = %v, want nil", err)
			}
			for _, substring := range tt.errContains {
				if !strings.Contains(err.Error(), substring) {
					t.Errorf("validate() error = %q, want it to contain %q", err, substring)
				}
			}
		})
	}
}

func TestNewValidatesUpstreamAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		route       Route
		wantMessage string
	}{
		{
			name: "valid upstream pool",
			route: Route{
				Name:       "users",
				Protocol:   ProtocolHTTP,
				PathPrefix: "/users",
				Upstream: &Upstream{
					LoadBalancing: LoadBalancingPolicyRoundRobin,
					Endpoints: []url.URL{
						*mustParseURL(t, "http://server-1:8080"),
						*mustParseURL(t, "https://server-2:8443"),
					},
				},
				ErrorResponses: map[int]ErrorResponse{
					http.StatusBadGateway: {Body: "upstream unavailable"},
				},
			},
		},
		{
			name: "unsupported protocol",
			route: Route{
				Name:       "users",
				Protocol:   Protocol("smtp"),
				PathPrefix: "/users",
				Upstream: &Upstream{
					LoadBalancing: LoadBalancingPolicyRoundRobin,
					Endpoints: []url.URL{
						*mustParseURL(t, "http://server-1:8080"),
					},
				},
			},
			wantMessage: `unsupported protocol "smtp"`,
		},
		{
			name: "invalid upstream pool",
			route: Route{
				Name:       "users",
				Protocol:   ProtocolHTTP,
				PathPrefix: "/users",
				Upstream: &Upstream{
					LoadBalancing: LoadBalancingPolicyRoundRobin,
				},
			},
			wantMessage: "upstream validation: at least one upstream endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := New([]Route{tt.route})
			if tt.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v, want nil", err)
				}
				if got == nil {
					t.Fatal("New() router = nil, want non-nil router")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", tt.wantMessage)
			}
			if got != nil {
				t.Errorf("New() router = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, tt.wantMessage)
			}
		})
	}
}
