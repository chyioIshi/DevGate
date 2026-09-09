package metrics

import (
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCircuitBreakerRouteMetricsRecordsEventsForBoundRoute(t *testing.T) {
	registry := prometheus.NewRegistry()
	breakerMetrics := NewCircuitBreaker(registry)
	usersMetrics := breakerMetrics.ForRoute("users")
	breakerMetrics.ForRoute("orders")

	usersMetrics.RecordStateTransition(
		proxy.CircuitStateClosed,
		proxy.CircuitStateOpen,
	)
	usersMetrics.RecordRejectedRequest()
	usersMetrics.RecordRejectedRequest()

	const want = `
# HELP devgate_circuit_breaker_state Current circuit breaker's state (0 = closed, 1 = open, 2 = half-open).
# TYPE devgate_circuit_breaker_state gauge
devgate_circuit_breaker_state{route="orders"} 0
devgate_circuit_breaker_state{route="users"} 1
# HELP devgate_circuit_breaker_transitions_total Total number of state transitions of the circuit breaker.
# TYPE devgate_circuit_breaker_transitions_total counter
devgate_circuit_breaker_transitions_total{from="closed",route="users",to="open"} 1
# HELP devgate_circuit_breaker_rejected_requests_total Total number of requests rejected by the circuit breaker.
# TYPE devgate_circuit_breaker_rejected_requests_total counter
devgate_circuit_breaker_rejected_requests_total{route="users"} 2
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered route metrics mismatch: %v", err)
	}
}
