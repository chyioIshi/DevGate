package metrics

import (
	"net/url"

	"github.com/chyioishi/devgate/internal/discovery"
)

// DNSDiscoveryRouteMetrics records DNS discovery metrics for one route.
type DNSDiscoveryRouteMetrics struct {
	dnsDiscovery *DNSDiscovery
	route        string
}

var _ discovery.DNSRefreshObserver = (*DNSDiscoveryRouteMetrics)(nil)

// ForRoute binds DNS discovery metrics to route and initializes its resolved
// endpoint count to zero.
func (m *DNSDiscovery) ForRoute(route string) *DNSDiscoveryRouteMetrics {
	metrics := &DNSDiscoveryRouteMetrics{
		dnsDiscovery: m,
		route:        route,
	}
	m.resolvedEndpoints.WithLabelValues(route).Set(0)
	return metrics
}

// RecordRefresh records the refresh outcome and, on success, the size of the
// applied endpoint snapshot.
func (m *DNSDiscoveryRouteMetrics) RecordRefresh(
	_ url.URL,
	endpointCount int,
	err error,
) {
	if err != nil {
		m.dnsDiscovery.RecordFailedRefresh(m.route)
		return
	}

	m.dnsDiscovery.RecordSuccessfulRefresh(m.route, endpointCount)
}
