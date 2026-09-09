package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCircuitBreakerExportsRegisteredMetricsPerRoute(t *testing.T) {
	registry := prometheus.NewRegistry()
	NewHTTP(registry).RequestStarted()
	breakerMetrics := NewCircuitBreaker(registry)

	for _, sample := range []struct {
		route       string
		state       int
		from        string
		to          string
		transitions int
		rejected    int
	}{
		{route: "users", state: 1, from: "closed", to: "open", transitions: 2, rejected: 5},
		{route: "orders", state: 0, from: "half_open", to: "closed", transitions: 1, rejected: 3},
	} {
		breakerMetrics.SetState(sample.route, sample.state)
		for range sample.transitions {
			breakerMetrics.RecordTransition(sample.route, sample.from, sample.to)
		}
		for range sample.rejected {
			breakerMetrics.RecordRejectedRequest(sample.route)
		}
	}
	breakerMetrics.SetState("orders", 2)
	breakerMetrics.SetState("orders", 0)

	const want = `
# HELP devgate_circuit_breaker_state Current circuit breaker's state (0 = closed, 1 = open, 2 = half-open).
# TYPE devgate_circuit_breaker_state gauge
devgate_circuit_breaker_state{route="orders"} 0
devgate_circuit_breaker_state{route="users"} 1
# HELP devgate_circuit_breaker_transitions_total Total number of state transitions of the circuit breaker.
# TYPE devgate_circuit_breaker_transitions_total counter
devgate_circuit_breaker_transitions_total{from="closed",route="users",to="open"} 2
devgate_circuit_breaker_transitions_total{from="half_open",route="orders",to="closed"} 1
# HELP devgate_circuit_breaker_rejected_requests_total Total number of requests rejected by the circuit breaker.
# TYPE devgate_circuit_breaker_rejected_requests_total counter
devgate_circuit_breaker_rejected_requests_total{route="orders"} 3
devgate_circuit_breaker_rejected_requests_total{route="users"} 5
# HELP devgate_http_requests_in_flight Number of HTTP requests currently in flight.
# TYPE devgate_http_requests_in_flight gauge
devgate_http_requests_in_flight 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered metrics mismatch: %v", err)
	}
}
