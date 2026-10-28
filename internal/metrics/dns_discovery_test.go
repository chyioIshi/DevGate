package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestDNSDiscoveryExportsRefreshesAndLatestSnapshotSize(t *testing.T) {
	registry := prometheus.NewRegistry()
	dnsDiscoveryMetrics := NewDNSDiscovery(registry)

	dnsDiscoveryMetrics.RecordSuccessfulRefresh("users", 3)
	dnsDiscoveryMetrics.RecordFailedRefresh("users")
	dnsDiscoveryMetrics.RecordSuccessfulRefresh("orders", 0)

	const want = `
# HELP devgate_dns_discovery_refreshes_total Total number of DNS discovery refresh attempts.
# TYPE devgate_dns_discovery_refreshes_total counter
devgate_dns_discovery_refreshes_total{outcome="failure",route="users"} 1
devgate_dns_discovery_refreshes_total{outcome="success",route="orders"} 1
devgate_dns_discovery_refreshes_total{outcome="success",route="users"} 1
# HELP devgate_dns_discovery_resolved_endpoints Number of endpoints in the latest successful DNS discovery snapshot.
# TYPE devgate_dns_discovery_resolved_endpoints gauge
devgate_dns_discovery_resolved_endpoints{route="orders"} 0
devgate_dns_discovery_resolved_endpoints{route="users"} 3
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered DNS discovery metrics mismatch: %v", err)
	}
}
