package main

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/discovery"
	"github.com/chyioishi/devgate/internal/router"
)

func TestNewRouteDNSWatcherReturnsNilWhenDNSDiscoveryIsDisabled(t *testing.T) {
	t.Parallel()

	watcher, err := newRouteDNSWatcher("users", &router.Upstream{}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("newRouteDNSWatcher() error = %v, want nil", err)
	}
	if watcher != nil {
		t.Errorf("newRouteDNSWatcher() watcher = %#v, want nil", watcher)
	}
}

func TestNewRouteDNSWatcherValidatesConfigurationAndDependencies(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://users.service.local:8080")
	validUpstream := func() *router.Upstream {
		return &router.Upstream{
			Endpoints: []url.URL{target},
			DNSDiscovery: &router.DNSDiscoveryPolicy{
				RefreshInterval: time.Minute,
				LookupTimeout:   time.Second,
			},
		}
	}
	resolver := routeEndpointResolverFunc(func(
		context.Context,
		url.URL,
	) ([]url.URL, error) {
		return nil, nil
	})
	applier := routeEndpointSnapshotApplierFunc(func([]url.URL) error { return nil })
	metricsObserver := noopDNSRefreshObserver{}
	logger := slog.New(slog.DiscardHandler)
	tests := []struct {
		name        string
		upstream    *router.Upstream
		resolver    discovery.EndpointResolver
		applier     discovery.EndpointSnapshotApplier
		metrics     discovery.DNSRefreshObserver
		logger      *slog.Logger
		wantMessage string
	}{
		{
			name:        "missing upstream",
			wantMessage: "route upstream was not provided",
		},
		{
			name: "missing logical endpoint",
			upstream: &router.Upstream{
				DNSDiscovery: &router.DNSDiscoveryPolicy{},
			},
			wantMessage: "exactly one endpoint",
		},
		{
			name: "multiple logical endpoints",
			upstream: &router.Upstream{
				Endpoints:    []url.URL{target, target},
				DNSDiscovery: &router.DNSDiscoveryPolicy{},
			},
			wantMessage: "exactly one endpoint",
		},
		{
			name:        "missing metrics observer",
			upstream:    validUpstream(),
			resolver:    resolver,
			applier:     applier,
			logger:      logger,
			wantMessage: "create route DNS refresh observer",
		},
		{
			name:        "missing logger",
			upstream:    validUpstream(),
			resolver:    resolver,
			applier:     applier,
			metrics:     metricsObserver,
			wantMessage: "create route DNS refresh observer",
		},
		{
			name:        "missing resolver",
			upstream:    validUpstream(),
			applier:     applier,
			metrics:     metricsObserver,
			logger:      logger,
			wantMessage: "create DNS watcher",
		},
		{
			name:        "missing applier",
			upstream:    validUpstream(),
			resolver:    resolver,
			metrics:     metricsObserver,
			logger:      logger,
			wantMessage: "create DNS watcher",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			watcher, err := newRouteDNSWatcher(
				"users",
				test.upstream,
				test.resolver,
				test.applier,
				test.metrics,
				test.logger,
			)
			if err == nil {
				t.Fatal("newRouteDNSWatcher() error = nil, want an error")
			}
			if watcher != nil {
				t.Errorf("newRouteDNSWatcher() watcher = %#v, want nil", watcher)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf(
					"newRouteDNSWatcher() error = %q, want context %q",
					err,
					test.wantMessage,
				)
			}
		})
	}
}

func TestNewRouteDNSWatcherWiresResolutionAndPublication(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://users.service.local:8080/api")
	resolved := []url.URL{
		mustParseObserverTarget(t, "http://192.0.2.1:8080/api"),
		mustParseObserverTarget(t, "http://192.0.2.2:8080/api"),
	}
	var gotTarget url.URL
	var gotEndpoints []url.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolver := routeEndpointResolverFunc(func(
		_ context.Context,
		got url.URL,
	) ([]url.URL, error) {
		gotTarget = got
		return resolved, nil
	})
	applier := routeEndpointSnapshotApplierFunc(func(got []url.URL) error {
		gotEndpoints = slices.Clone(got)
		cancel()
		return nil
	})
	watcher, err := newRouteDNSWatcher(
		"users",
		&router.Upstream{
			Endpoints: []url.URL{target},
			DNSDiscovery: &router.DNSDiscoveryPolicy{
				RefreshInterval: time.Hour,
				LookupTimeout:   time.Second,
			},
		},
		resolver,
		applier,
		noopDNSRefreshObserver{},
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("newRouteDNSWatcher() error = %v", err)
	}

	watcher.Run(ctx)

	if gotTarget != target {
		t.Errorf("resolver target = %v, want %v", gotTarget, target)
	}
	if !slices.Equal(gotEndpoints, resolved) {
		t.Errorf("applied endpoints = %v, want %v", gotEndpoints, resolved)
	}
}

type routeEndpointResolverFunc func(context.Context, url.URL) ([]url.URL, error)

func (f routeEndpointResolverFunc) Resolve(
	ctx context.Context,
	target url.URL,
) ([]url.URL, error) {
	return f(ctx, target)
}

type routeEndpointSnapshotApplierFunc func([]url.URL) error

func (f routeEndpointSnapshotApplierFunc) Apply(endpoints []url.URL) error {
	return f(endpoints)
}

type noopDNSRefreshObserver struct{}

func (noopDNSRefreshObserver) RecordRefresh(url.URL, int, error) {}
