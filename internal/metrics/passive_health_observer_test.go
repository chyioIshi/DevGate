package metrics

import (
	"net/url"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPassiveHealthRouteMetricsRecordsEventsForBoundRoute(t *testing.T) {
	registry := prometheus.NewRegistry()
	passiveHealthMetrics := NewPassiveHealth(registry)

	usersTarget, err := url.Parse("http://user:secret@users:8080")
	if err != nil {
		t.Fatalf("parse users target: %v", err)
	}
	ordersTarget, err := url.Parse("http://orders:8080")
	if err != nil {
		t.Fatalf("parse orders target: %v", err)
	}

	routeMetrics := passiveHealthMetrics.ForRoute(
		"api",
		[]url.URL{*usersTarget, *ordersTarget},
	)
	routeMetrics.RecordEjection(*usersTarget)
	routeMetrics.RecordEjection(*usersTarget)
	routeMetrics.RecordEjection(*ordersTarget)
	routeMetrics.RecordRecovery(*ordersTarget)

	const want = `
# HELP devgate_passive_health_check_ejections_total Total number of passive upstream endpoint ejections.
# TYPE devgate_passive_health_check_ejections_total counter
devgate_passive_health_check_ejections_total{route="api",target="http://orders:8080"} 1
devgate_passive_health_check_ejections_total{route="api",target="http://user:xxxxx@users:8080"} 2
# HELP devgate_passive_health_check_endpoint_ejected Passive ejection state (1 for ejected, 0 for available).
# TYPE devgate_passive_health_check_endpoint_ejected gauge
devgate_passive_health_check_endpoint_ejected{route="api",target="http://orders:8080"} 0
devgate_passive_health_check_endpoint_ejected{route="api",target="http://user:xxxxx@users:8080"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered passive health route metrics mismatch: %v", err)
	}
}
