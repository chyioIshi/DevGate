package main

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
)

func TestRoutesFromConfig(t *testing.T) {
	routeConfigs := []config.RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: "http://users-service:8080",
		},
		{
			Name:        "greeter",
			Protocol:    "grpc",
			PathPrefix:  "/greeter.v1.Greeter",
			UpstreamURL: "http://greeter-service:9090",
		},
	}
	want := []struct {
		name        string
		protocol    router.Protocol
		pathPrefix  string
		upstreamURL string
	}{
		{
			name:        "users",
			protocol:    router.ProtocolHTTP,
			pathPrefix:  "/api/users",
			upstreamURL: "http://users-service:8080",
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
