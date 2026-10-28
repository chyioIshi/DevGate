package metrics

import (
	"errors"
	"net/url"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestDNSDiscoveryRouteMetricsRecordsRefreshLifecycle(t *testing.T) {
	registry := prometheus.NewRegistry()
	dnsDiscoveryMetrics := NewDNSDiscovery(registry)
	routeMetrics := dnsDiscoveryMetrics.ForRoute("users")

	if got := testutil.ToFloat64(
		dnsDiscoveryMetrics.resolvedEndpoints.WithLabelValues("users"),
	); got != 0 {
		t.Errorf("initial resolved endpoint count = %v, want 0", got)
	}

	routeMetrics.RecordRefresh(url.URL{}, 4, nil)
	routeMetrics.RecordRefresh(url.URL{}, 0, errors.New("lookup failed"))

	if got := testutil.ToFloat64(
		dnsDiscoveryMetrics.refreshesTotal.WithLabelValues(
			"users",
			dnsDiscoveryOutcomeSuccess,
		),
	); got != 1 {
		t.Errorf("successful refresh count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(
		dnsDiscoveryMetrics.refreshesTotal.WithLabelValues(
			"users",
			dnsDiscoveryOutcomeFailure,
		),
	); got != 1 {
		t.Errorf("failed refresh count = %v, want 1", got)
	}
	if got := testutil.ToFloat64(
		dnsDiscoveryMetrics.resolvedEndpoints.WithLabelValues("users"),
	); got != 4 {
		t.Errorf("resolved endpoint count after failure = %v, want 4", got)
	}
}
