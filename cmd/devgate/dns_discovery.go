package main

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/chyioishi/devgate/internal/discovery"
	"github.com/chyioishi/devgate/internal/router"
)

func newRouteDNSWatcher(
	routeName string,
	routeUpstream *router.Upstream,
	endpointResolver discovery.EndpointResolver,
	endpointApplier discovery.EndpointSnapshotApplier,
	metricsObserver discovery.DNSRefreshObserver,
	logger *slog.Logger,
) (*discovery.DNSWatcher, error) {
	if routeUpstream == nil {
		return nil, errors.New("route upstream was not provided")
	}
	dnsDiscovery := routeUpstream.DNSDiscovery
	if dnsDiscovery == nil {
		return nil, nil
	}
	if len(routeUpstream.Endpoints) != 1 {
		return nil, fmt.Errorf("route upstream must have exactly one endpoint for route %q", routeName)
	}

	observer, err := newRouteDNSRefreshObserver(
		routeName,
		metricsObserver,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create route DNS refresh observer for route %q: %w",
			routeName,
			err,
		)
	}

	dnsWatcher, err := discovery.NewDNSWatcher(
		endpointResolver,
		endpointApplier,
		observer,
		routeUpstream.Endpoints[0],
		dnsDiscovery.RefreshInterval,
		dnsDiscovery.LookupTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("create DNS watcher for route %q: %w", routeName, err)
	}

	return dnsWatcher, nil
}
