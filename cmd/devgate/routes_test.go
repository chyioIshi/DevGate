package main

import (
	"errors"
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
			Name:                "users",
			Protocol:            "http",
			PathPrefix:          "/api/users",
			Methods:             []string{"GET", "POST"},
			UpstreamURL:         "http://users-service:8080",
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
	}
	want := []struct {
		name                string
		protocol            router.Protocol
		pathPrefix          string
		methods             []string
		upstreamURL         string
		requestHeaders      *router.HeaderTransformPolicy
		responseHeaders     *router.HeaderTransformPolicy
		rateLimit           *router.RateLimitPolicy
		stripPrefix         bool
		requestTimeout      time.Duration
		maxRequestBodyBytes int64
	}{
		{
			name:        "users",
			protocol:    router.ProtocolHTTP,
			pathPrefix:  "/api/users",
			methods:     []string{"GET", "POST"},
			upstreamURL: "http://users-service:8080",
			requestHeaders: &router.HeaderTransformPolicy{
				Set:    map[string]string{"X-Gateway": "DevGate"},
				Remove: []string{"X-Legacy-Header"},
			},
			responseHeaders: &router.HeaderTransformPolicy{
				Set:    map[string]string{"X-Gateway-Response": "DevGate"},
				Remove: []string{"X-Legacy-Response-Header"},
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
			name:        "greeter",
			protocol:    router.ProtocolGRPC,
			pathPrefix:  "/greeter.v1.Greeter",
			upstreamURL: "http://greeter-service:9090",
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
		if !slices.Equal(got[i].Methods, want[i].methods) {
			t.Errorf("route[%d].Methods = %q, want %q", i, got[i].Methods, want[i].methods)
		}
		if got[i].UpstreamURL == nil {
			t.Errorf("route[%d].UpstreamURL = nil", i)
			continue
		}
		if got[i].UpstreamURL.String() != want[i].upstreamURL {
			t.Errorf(
				"route[%d].UpstreamURL = %q, want %q",
				i,
				got[i].UpstreamURL,
				want[i].upstreamURL,
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
