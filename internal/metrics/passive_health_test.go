package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPassiveHealthExportsEjectionsAndEndpointState(t *testing.T) {
	registry := prometheus.NewRegistry()
	passiveHealthMetrics := NewPassiveHealth(registry)

	for range 2 {
		passiveHealthMetrics.RecordEjection("users", "http://users:8080")
	}
	passiveHealthMetrics.RecordEjection("orders", "http://orders:8080")

	passiveHealthMetrics.SetEndpointEjected("users", "http://users:8080", true)
	passiveHealthMetrics.SetEndpointEjected("orders", "http://orders:8080", true)
	passiveHealthMetrics.SetEndpointEjected("orders", "http://orders:8080", false)

	const want = `
# HELP devgate_passive_health_check_ejections_total Total number of passive upstream endpoint ejections.
# TYPE devgate_passive_health_check_ejections_total counter
devgate_passive_health_check_ejections_total{route="orders",target="http://orders:8080"} 1
devgate_passive_health_check_ejections_total{route="users",target="http://users:8080"} 2
# HELP devgate_passive_health_check_endpoint_ejected Passive ejection state (1 for ejected, 0 for available).
# TYPE devgate_passive_health_check_endpoint_ejected gauge
devgate_passive_health_check_endpoint_ejected{route="orders",target="http://orders:8080"} 0
devgate_passive_health_check_endpoint_ejected{route="users",target="http://users:8080"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered passive health metrics mismatch: %v", err)
	}
}
