package main

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
)

func TestRoutesFromConfig(t *testing.T) {
	routeConfigs := []config.RouteConfig{
		{
			Name:       "users",
			Protocol:   "http",
			PathPrefix: "/api/users",
			Methods:    []string{"GET", "POST"},
			Hosts:      []string{"api.example.com", "api.internal"},
			Priority:   100,
			HeaderMatches: []config.HeaderMatchConfig{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
			Upstream: &config.UpstreamConfig{
				LoadBalancing: config.LoadBalancingPolicyRoundRobin,
				Discovery: &config.UpstreamDiscoveryConfig{
					Static: &config.StaticUpstreamDiscoveryConfig{
						Endpoints: []config.UpstreamEndpointConfig{
							{URL: "http://users-service-1:8080"},
							{URL: "http://users-service-2:8080"},
						},
					},
				},
			},
			StripPathPrefix:     true,
			RequestTimeout:      2500 * time.Millisecond,
			MaxRequestBodyBytes: 10 * 1024 * 1024,
			RequestHeaders: &config.HeaderTransformConfig{
				Set:    map[string]string{"X-Gateway": "DevGate"},
				Remove: []string{"X-Legacy-Header"},
			},
			ResponseHeaders: &config.HeaderTransformConfig{
				Set:    map[string]string{"X-Gateway-Response": "DevGate"},
				Remove: []string{"X-Legacy-Response-Header"},
			},
			ErrorResponses: map[int]config.ErrorResponseConfig{
				http.StatusBadGateway: {
					Body:    `{"error":"upstream unavailable"}`,
					Headers: map[string]string{"Content-Type": "application/json"},
				},
			},
			RateLimit: &config.RateLimitConfig{
				RequestsPerSecond: 12.5,
				Burst:             25,
			},
		},
		{
			Name:        "greeter",
			Protocol:    "grpc",
			PathPrefix:  "/greeter.v1.Greeter",
			UpstreamURL: "http://greeter-service:9090",
		},
		{
			Name:        "health",
			Protocol:    "http",
			PathExact:   "/healthz",
			Priority:    -10,
			UpstreamURL: "http://health-service:8080",
		},
		{
			Name:       "maintenance",
			PathPrefix: "/api",
			DirectResponse: &config.DirectResponseConfig{
				StatusCode: http.StatusServiceUnavailable,
				Body:       "service temporarily unavailable",
			},
		},
		{
			Name:      "legacy-api",
			PathExact: "/legacy",
			Redirect: &config.RedirectConfig{
				StatusCode: http.StatusPermanentRedirect,
				Location:   "/api/v2",
			},
		},
	}
	want := []struct {
		name                string
		protocol            router.Protocol
		pathPrefix          string
		pathExact           string
		methods             []string
		hosts               []string
		headerMatches       []router.HeaderMatch
		priority            int
		upstreamURLs        []string
		directResponse      *router.DirectResponse
		redirect            *router.Redirect
		errorResponses      map[int]router.ErrorResponse
		requestHeaders      *router.HeaderTransformPolicy
		responseHeaders     *router.HeaderTransformPolicy
		rateLimit           *router.RateLimitPolicy
		stripPrefix         bool
		requestTimeout      time.Duration
		maxRequestBodyBytes int64
	}{
		{
			name:       "users",
			protocol:   router.ProtocolHTTP,
			pathPrefix: "/api/users",
			methods:    []string{"GET", "POST"},
			hosts:      []string{"api.example.com", "api.internal"},
			priority:   100,
			headerMatches: []router.HeaderMatch{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
			upstreamURLs: []string{
				"http://users-service-1:8080",
				"http://users-service-2:8080",
			},
			requestHeaders: &router.HeaderTransformPolicy{
				Set:    map[string]string{"X-Gateway": "DevGate"},
				Remove: []string{"X-Legacy-Header"},
			},
			responseHeaders: &router.HeaderTransformPolicy{
				Set:    map[string]string{"X-Gateway-Response": "DevGate"},
				Remove: []string{"X-Legacy-Response-Header"},
			},
			errorResponses: map[int]router.ErrorResponse{
				http.StatusBadGateway: {
					Body:    `{"error":"upstream unavailable"}`,
					Headers: map[string]string{"Content-Type": "application/json"},
				},
			},
			rateLimit: &router.RateLimitPolicy{
				RequestsPerSecond: 12.5,
				Burst:             25,
			},
			stripPrefix:         true,
			requestTimeout:      2500 * time.Millisecond,
			maxRequestBodyBytes: 10 * 1024 * 1024,
		},
		{
			name:         "greeter",
			protocol:     router.ProtocolGRPC,
			pathPrefix:   "/greeter.v1.Greeter",
			upstreamURLs: []string{"http://greeter-service:9090"},
		},
		{
			name:         "health",
			protocol:     router.ProtocolHTTP,
			pathExact:    "/healthz",
			priority:     -10,
			upstreamURLs: []string{"http://health-service:8080"},
		},
		{
			name:       "maintenance",
			pathPrefix: "/api",
			directResponse: &router.DirectResponse{
				StatusCode: http.StatusServiceUnavailable,
				Body:       "service temporarily unavailable",
			},
		},
		{
			name:      "legacy-api",
			pathExact: "/legacy",
			redirect: &router.Redirect{
				StatusCode: http.StatusPermanentRedirect,
				Location:   "/api/v2",
			},
		},
	}

	got, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("routesFromConfig() routes length = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i].Name != want[i].name {
			t.Errorf("route[%d].Name = %q, want %q", i, got[i].Name, want[i].name)
		}
		if got[i].Protocol != want[i].protocol {
			t.Errorf("route[%d].Protocol = %q, want %q", i, got[i].Protocol, want[i].protocol)
		}
		if got[i].PathPrefix != want[i].pathPrefix {
			t.Errorf("route[%d].PathPrefix = %q, want %q", i, got[i].PathPrefix, want[i].pathPrefix)
		}
		if got[i].PathExact != want[i].pathExact {
			t.Errorf("route[%d].PathExact = %q, want %q", i, got[i].PathExact, want[i].pathExact)
		}
		if !slices.Equal(got[i].Methods, want[i].methods) {
			t.Errorf("route[%d].Methods = %q, want %q", i, got[i].Methods, want[i].methods)
		}
		if !slices.Equal(got[i].Hosts, want[i].hosts) {
			t.Errorf("route[%d].Hosts = %q, want %q", i, got[i].Hosts, want[i].hosts)
		}
		if !slices.Equal(got[i].HeaderMatches, want[i].headerMatches) {
			t.Errorf(
				"route[%d].HeaderMatches = %+v, want %+v",
				i,
				got[i].HeaderMatches,
				want[i].headerMatches,
			)
		}
		if got[i].Priority != want[i].priority {
			t.Errorf("route[%d].Priority = %d, want %d", i, got[i].Priority, want[i].priority)
		}
		if len(want[i].upstreamURLs) == 0 {
			if got[i].Upstream != nil {
				t.Errorf("route[%d].Upstream = %+v, want nil", i, got[i].Upstream)
			}
		} else if got[i].Upstream == nil {
			t.Errorf("route[%d].Upstream = nil, want endpoints %v", i, want[i].upstreamURLs)
		} else {
			if got[i].Upstream.LoadBalancing != router.LoadBalancingPolicyRoundRobin {
				t.Errorf(
					"route[%d].Upstream.LoadBalancing = %q, want %q",
					i,
					got[i].Upstream.LoadBalancing,
					router.LoadBalancingPolicyRoundRobin,
				)
			}
			gotURLs := make([]string, len(got[i].Upstream.Endpoints))
			for endpointIndex := range got[i].Upstream.Endpoints {
				gotURLs[endpointIndex] = got[i].Upstream.Endpoints[endpointIndex].String()
			}
			if !slices.Equal(gotURLs, want[i].upstreamURLs) {
				t.Errorf(
					"route[%d].Upstream.Endpoints = %v, want %v",
					i,
					gotURLs,
					want[i].upstreamURLs,
				)
			}
		}
		if !reflect.DeepEqual(got[i].DirectResponse, want[i].directResponse) {
			t.Errorf(
				"route[%d].DirectResponse = %+v, want %+v",
				i,
				got[i].DirectResponse,
				want[i].directResponse,
			)
		}
		if !reflect.DeepEqual(got[i].Redirect, want[i].redirect) {
			t.Errorf(
				"route[%d].Redirect = %+v, want %+v",
				i,
				got[i].Redirect,
				want[i].redirect,
			)
		}
		if !reflect.DeepEqual(got[i].ErrorResponses, want[i].errorResponses) {
			t.Errorf(
				"route[%d].ErrorResponses = %+v, want %+v",
				i,
				got[i].ErrorResponses,
				want[i].errorResponses,
			)
		}
		if !reflect.DeepEqual(got[i].RateLimit, want[i].rateLimit) {
			t.Errorf("route[%d].RateLimit = %+v, want %+v", i, got[i].RateLimit, want[i].rateLimit)
		}
		if !reflect.DeepEqual(got[i].RequestHeaders, want[i].requestHeaders) {
			t.Errorf(
				"route[%d].RequestHeaders = %+v, want %+v",
				i,
				got[i].RequestHeaders,
				want[i].requestHeaders,
			)
		}
		if !reflect.DeepEqual(got[i].ResponseHeaders, want[i].responseHeaders) {
			t.Errorf(
				"route[%d].ResponseHeaders = %+v, want %+v",
				i,
				got[i].ResponseHeaders,
				want[i].responseHeaders,
			)
		}
		if got[i].StripPathPrefix != want[i].stripPrefix {
			t.Errorf(
				"route[%d].StripPathPrefix = %t, want %t",
				i,
				got[i].StripPathPrefix,
				want[i].stripPrefix,
			)
		}
		if got[i].RequestTimeout != want[i].requestTimeout {
			t.Errorf(
				"route[%d].RequestTimeout = %s, want %s",
				i,
				got[i].RequestTimeout,
				want[i].requestTimeout,
			)
		}
		if got[i].MaxRequestBodyBytes != want[i].maxRequestBodyBytes {
			t.Errorf(
				"route[%d].MaxRequestBodyBytes = %d, want %d",
				i,
				got[i].MaxRequestBodyBytes,
				want[i].maxRequestBodyBytes,
			)
		}
	}
}

func TestRoutesFromConfigCopiesMethods(t *testing.T) {
	methods := []string{"GET", "POST"}
	routeConfigs := []config.RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			Methods:     methods,
			UpstreamURL: "http://users-service:8080",
		},
	}

	routes, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}

	methods[0] = "DELETE"
	if got := routes[0].Methods[0]; got != "GET" {
		t.Errorf("route method after config mutation = %q, want %q", got, "GET")
	}
}

func TestRoutesFromConfigCopiesHosts(t *testing.T) {
	hosts := []string{"api.example.com", "api.internal"}
	routeConfigs := []config.RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			Hosts:       hosts,
			UpstreamURL: "http://users-service:8080",
		},
	}

	routes, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}

	hosts[0] = "attacker.example"
	if got := routes[0].Hosts[0]; got != "api.example.com" {
		t.Errorf("route host after config mutation = %q, want %q", got, "api.example.com")
	}
}

func TestRoutesFromConfigCopiesHeaderMatches(t *testing.T) {
	headerMatches := []config.HeaderMatchConfig{
		{Name: "X-Environment", Exact: "production"},
	}
	routeConfigs := []config.RouteConfig{
		{
			Name:          "users",
			Protocol:      "http",
			PathPrefix:    "/api/users",
			HeaderMatches: headerMatches,
			UpstreamURL:   "http://users-service:8080",
		},
	}

	routes, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}

	headerMatches[0].Exact = "staging"
	if got := routes[0].HeaderMatches[0].Exact; got != "production" {
		t.Errorf("route header match after config mutation = %q, want %q", got, "production")
	}
}

func TestRoutesFromConfigCopiesHeaderPolicies(t *testing.T) {
	headerConfig := &config.HeaderTransformConfig{
		Set:    map[string]string{"X-Gateway": "DevGate"},
		Remove: []string{"X-Legacy-Header"},
	}
	routeConfigs := []config.RouteConfig{
		{
			Name:            "users",
			Protocol:        "http",
			PathPrefix:      "/api/users",
			UpstreamURL:     "http://users-service:8080",
			RequestHeaders:  headerConfig,
			ResponseHeaders: headerConfig,
		},
	}

	got, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}

	headerConfig.Set["X-Gateway"] = "mutated"
	headerConfig.Remove[0] = "X-Mutated"

	if value := got[0].RequestHeaders.Set["X-Gateway"]; value != "DevGate" {
		t.Errorf("runtime set value = %q, want %q", value, "DevGate")
	}
	if name := got[0].RequestHeaders.Remove[0]; name != "X-Legacy-Header" {
		t.Errorf("runtime remove header = %q, want %q", name, "X-Legacy-Header")
	}
	if value := got[0].ResponseHeaders.Set["X-Gateway"]; value != "DevGate" {
		t.Errorf("runtime response set value = %q, want %q", value, "DevGate")
	}
	if name := got[0].ResponseHeaders.Remove[0]; name != "X-Legacy-Header" {
		t.Errorf("runtime response remove header = %q, want %q", name, "X-Legacy-Header")
	}

	got[0].RequestHeaders.Set["X-Gateway"] = "request-mutated"
	got[0].RequestHeaders.Remove[0] = "X-Request-Mutated"
	if value := got[0].ResponseHeaders.Set["X-Gateway"]; value != "DevGate" {
		t.Errorf("response set value after request mutation = %q, want %q", value, "DevGate")
	}
	if name := got[0].ResponseHeaders.Remove[0]; name != "X-Legacy-Header" {
		t.Errorf("response remove header after request mutation = %q, want %q", name, "X-Legacy-Header")
	}
}

func TestRoutesFromConfigCopiesErrorResponses(t *testing.T) {
	errorResponses := map[int]config.ErrorResponseConfig{
		http.StatusBadGateway: {
			Body:    `{"error":"upstream unavailable"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
	}
	routeConfigs := []config.RouteConfig{
		{
			Name:           "users",
			Protocol:       "http",
			PathPrefix:     "/api/users",
			UpstreamURL:    "http://users-service:8080",
			ErrorResponses: errorResponses,
		},
	}

	routes, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}

	response := errorResponses[http.StatusBadGateway]
	response.Headers["Content-Type"] = "text/plain"
	response.Body = "mutated"
	errorResponses[http.StatusBadGateway] = response
	errorResponses[http.StatusGatewayTimeout] = config.ErrorResponseConfig{Body: "timeout"}

	got := routes[0].ErrorResponses
	if len(got) != 1 {
		t.Fatalf("runtime error responses length = %d, want 1", len(got))
	}
	if gotResponse := got[http.StatusBadGateway]; gotResponse.Body != `{"error":"upstream unavailable"}` {
		t.Errorf(
			"runtime error response body after config mutation = %q, want %q",
			gotResponse.Body,
			`{"error":"upstream unavailable"}`,
		)
	}
	if gotHeader := got[http.StatusBadGateway].Headers["Content-Type"]; gotHeader != "application/json" {
		t.Errorf(
			"runtime Content-Type after config mutation = %q, want %q",
			gotHeader,
			"application/json",
		)
	}
}

func TestRoutesFromConfigReturnsNilAfterParseError(t *testing.T) {
	routeConfigs := []config.RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: "http://users-service:8080",
		},
		{
			Name:        "broken",
			Protocol:    "http",
			PathPrefix:  "/broken",
			UpstreamURL: "://broken",
		},
	}

	got, err := routesFromConfig(routeConfigs)
	if err == nil {
		t.Fatal("routesFromConfig() error = nil, want URL parsing error")
	}
	if got != nil {
		t.Errorf("routesFromConfig() routes = %+v, want nil", got)
	}
	if !strings.Contains(err.Error(), `parse upstream URL for route "broken"`) {
		t.Errorf("routesFromConfig() error = %q, want route context", err)
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Errorf("routesFromConfig() error = %v, want *url.Error", err)
	}
}

func TestUpstreamFromConfig(t *testing.T) {
	t.Parallel()

	staticUpstream := func(
		policy config.LoadBalancingPolicy,
		endpoints ...string,
	) *config.UpstreamConfig {
		endpointConfigs := make([]config.UpstreamEndpointConfig, 0, len(endpoints))
		for _, endpoint := range endpoints {
			endpointConfigs = append(endpointConfigs, config.UpstreamEndpointConfig{URL: endpoint})
		}
		return &config.UpstreamConfig{
			LoadBalancing: policy,
			Discovery: &config.UpstreamDiscoveryConfig{
				Static: &config.StaticUpstreamDiscoveryConfig{
					Endpoints: endpointConfigs,
				},
			},
		}
	}

	tests := []struct {
		name          string
		route         config.RouteConfig
		wantNil       bool
		wantPolicy    router.LoadBalancingPolicy
		wantEndpoints []string
		wantMessage   string
	}{
		{
			name:    "no upstream",
			route:   config.RouteConfig{Name: "health"},
			wantNil: true,
		},
		{
			name: "legacy upstream URL",
			route: config.RouteConfig{
				Name:        "users",
				UpstreamURL: "http://users-service:8080",
			},
			wantEndpoints: []string{"http://users-service:8080"},
		},
		{
			name: "whitespace legacy URL is not treated as absent",
			route: config.RouteConfig{
				Name:        "users",
				UpstreamURL: "   ",
			},
			wantEndpoints: []string{"%20%20%20"},
		},
		{
			name: "legacy and new upstream conflict",
			route: config.RouteConfig{
				Name:        "users",
				UpstreamURL: "http://legacy:8080",
				Upstream:    staticUpstream("", "http://server-1:8080"),
			},
			wantMessage: "both upstream and upstream URL",
		},
		{
			name: "new upstream uses default policy",
			route: config.RouteConfig{
				Name:     "users",
				Upstream: staticUpstream("", "http://server-1:8080", "https://server-2:8443"),
			},
			wantEndpoints: []string{"http://server-1:8080", "https://server-2:8443"},
		},
		{
			name: "new upstream uses explicit round robin policy",
			route: config.RouteConfig{
				Name: "users",
				Upstream: staticUpstream(
					config.LoadBalancingPolicyRoundRobin,
					"http://server-1:8080",
				),
			},
			wantEndpoints: []string{"http://server-1:8080"},
		},
		{
			name: "new upstream uses random policy",
			route: config.RouteConfig{
				Name: "users",
				Upstream: staticUpstream(
					config.LoadBalancingPolicyRandom,
					"http://server-1:8080",
					"http://server-2:8080",
				),
			},
			wantPolicy:    router.LoadBalancingPolicyRandom,
			wantEndpoints: []string{"http://server-1:8080", "http://server-2:8080"},
		},
		{
			name: "unsupported policy",
			route: config.RouteConfig{
				Name:     "users",
				Upstream: staticUpstream("least_connections", "http://server-1:8080"),
			},
			wantMessage: `invalid load balancing policy "least_connections" for route "users"`,
		},
		{
			name: "missing discovery",
			route: config.RouteConfig{
				Name:     "users",
				Upstream: &config.UpstreamConfig{},
			},
			wantMessage: `no discovery configuration for upstream of route "users"`,
		},
		{
			name: "missing static discovery",
			route: config.RouteConfig{
				Name: "users",
				Upstream: &config.UpstreamConfig{
					Discovery: &config.UpstreamDiscoveryConfig{},
				},
			},
			wantMessage: `no static discovery configuration for upstream of route "users"`,
		},
		{
			name: "invalid second endpoint",
			route: config.RouteConfig{
				Name:     "users",
				Upstream: staticUpstream("", "http://server-1:8080", "://broken"),
			},
			wantMessage: `parse upstream endpoint [1] for route "users"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := upstreamFromConfig(tt.route)
			if tt.wantMessage != "" {
				if err == nil {
					t.Fatalf("upstreamFromConfig() error = nil, want context %q", tt.wantMessage)
				}
				if got != nil {
					t.Errorf("upstreamFromConfig() upstream = %#v, want nil", got)
				}
				if !strings.Contains(err.Error(), tt.wantMessage) {
					t.Errorf("upstreamFromConfig() error = %q, want context %q", err, tt.wantMessage)
				}
				return
			}

			if err != nil {
				t.Fatalf("upstreamFromConfig() error = %v, want nil", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("upstreamFromConfig() upstream = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("upstreamFromConfig() upstream = nil, want non-nil upstream")
			}
			wantPolicy := tt.wantPolicy
			if wantPolicy == "" {
				wantPolicy = router.LoadBalancingPolicyRoundRobin
			}
			if got.LoadBalancing != wantPolicy {
				t.Errorf(
					"upstreamFromConfig() policy = %q, want %q",
					got.LoadBalancing,
					wantPolicy,
				)
			}
			gotEndpoints := make([]string, len(got.Endpoints))
			for i := range got.Endpoints {
				gotEndpoints[i] = got.Endpoints[i].String()
			}
			if !slices.Equal(gotEndpoints, tt.wantEndpoints) {
				t.Errorf(
					"upstreamFromConfig() endpoints = %v, want %v",
					gotEndpoints,
					tt.wantEndpoints,
				)
			}
		})
	}
}
