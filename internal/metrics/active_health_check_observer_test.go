package metrics

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestActiveHealthCheckRouteMetricsRecordsEventsForBoundRoute(t *testing.T) {
	registry := prometheus.NewRegistry()
	healthCheckMetrics := NewActiveHealthCheck(registry)
	usersTarget := mustParseMetricsURL(t, "http://user:secret@users:8080")
	ordersTarget := mustParseMetricsURL(t, "http://orders:8080")
	usersMetrics := healthCheckMetrics.ForRoute(
		"users",
		[]url.URL{usersTarget, ordersTarget},
	)

	usersMetrics.RecordProbeResult(usersTarget, true, nil)
	usersMetrics.RecordProbeResult(
		ordersTarget,
		false,
		errors.New("probe response contained a secret"),
	)
	usersMetrics.RecordStateTransition(ordersTarget, false)

	const want = `
# HELP devgate_active_health_check_probes_total Total number of active health check probes.
# TYPE devgate_active_health_check_probes_total counter
devgate_active_health_check_probes_total{outcome="failure",route="users",target="http://orders:8080"} 1
devgate_active_health_check_probes_total{outcome="success",route="users",target="http://user:xxxxx@users:8080"} 1
# HELP devgate_active_health_check_endpoint_healthy Whether the endpoint is healthy (1 for healthy, 0 for unhealthy).
# TYPE devgate_active_health_check_endpoint_healthy gauge
devgate_active_health_check_endpoint_healthy{route="users",target="http://orders:8080"} 0
devgate_active_health_check_endpoint_healthy{route="users",target="http://user:xxxxx@users:8080"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered route metrics mismatch: %v", err)
	}
}

func mustParseMetricsURL(t *testing.T, rawURL string) url.URL {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse metrics URL %q: %v", rawURL, err)
	}
	return *parsedURL
}
