package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestActiveHealthCheckExportsProbesAndEndpointState(t *testing.T) {
	registry := prometheus.NewRegistry()
	healthCheckMetrics := NewActiveHealthCheck(registry)

	for range 2 {
		healthCheckMetrics.RecordSuccessfulProbe("users", "http://users:8080")
	}
	healthCheckMetrics.RecordFailedProbe("users", "http://users:8080")
	healthCheckMetrics.RecordFailedProbe("orders", "http://orders:8080")

	healthCheckMetrics.SetEndpointHealthy("users", "http://users:8080", true)
	healthCheckMetrics.SetEndpointHealthy("orders", "http://orders:8080", true)
	healthCheckMetrics.SetEndpointHealthy("orders", "http://orders:8080", false)

	const want = `
# HELP devgate_active_health_check_probes_total Total number of active health check probes.
# TYPE devgate_active_health_check_probes_total counter
devgate_active_health_check_probes_total{outcome="failure",route="orders",target="http://orders:8080"} 1
devgate_active_health_check_probes_total{outcome="failure",route="users",target="http://users:8080"} 1
devgate_active_health_check_probes_total{outcome="success",route="users",target="http://users:8080"} 2
# HELP devgate_active_health_check_endpoint_healthy Whether the endpoint is healthy (1 for healthy, 0 for unhealthy).
# TYPE devgate_active_health_check_endpoint_healthy gauge
devgate_active_health_check_endpoint_healthy{route="orders",target="http://orders:8080"} 0
devgate_active_health_check_endpoint_healthy{route="users",target="http://users:8080"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered active health check metrics mismatch: %v", err)
	}
}
