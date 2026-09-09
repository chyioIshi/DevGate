package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	circuitBreakerSubsystem = "circuit_breaker"

	stateMetricName                 = "state"
	transitionsTotalMetricName      = "transitions_total"
	rejectedRequestsTotalMetricName = "rejected_requests_total"
)

// CircuitBreaker collects per-route circuit breaker states, transitions, and
// rejected requests.
type CircuitBreaker struct {
	state                 *prometheus.GaugeVec
	transitionsTotal      *prometheus.CounterVec
	rejectedRequestsTotal *prometheus.CounterVec
}

// NewCircuitBreaker creates circuit breaker metrics and registers their collectors
// with register. It panics if registration fails.
func NewCircuitBreaker(register prometheus.Registerer) *CircuitBreaker {
	stateCollectorOpts := prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: circuitBreakerSubsystem,
		Name:      stateMetricName,
		Help:      "Current circuit breaker's state (0 = closed, 1 = open, 2 = half-open).",
	}
	transitionsTotalCollectorOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: circuitBreakerSubsystem,
		Name:      transitionsTotalMetricName,
		Help:      "Total number of state transitions of the circuit breaker.",
	}
	rejectedRequestsTotalCollectorOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: circuitBreakerSubsystem,
		Name:      rejectedRequestsTotalMetricName,
		Help:      "Total number of requests rejected by the circuit breaker.",
	}
	stateCollector := prometheus.NewGaugeVec(
		stateCollectorOpts,
		[]string{"route"},
	)
	transitionsTotalCollector := prometheus.NewCounterVec(
		transitionsTotalCollectorOpts,
		[]string{"route", "from", "to"},
	)
	rejectedRequestsTotalCollector := prometheus.NewCounterVec(
		rejectedRequestsTotalCollectorOpts,
		[]string{"route"},
	)
	register.MustRegister(
		stateCollector,
		transitionsTotalCollector,
		rejectedRequestsTotalCollector,
	)
	return &CircuitBreaker{
		state:                 stateCollector,
		transitionsTotal:      transitionsTotalCollector,
		rejectedRequestsTotal: rejectedRequestsTotalCollector,
	}
}

// RecordRejectedRequest records one request rejected by the circuit breaker for route.
func (m *CircuitBreaker) RecordRejectedRequest(route string) {
	m.rejectedRequestsTotal.WithLabelValues(route).Inc()
}

// SetState sets the current circuit breaker state for route. The caller must supply
// 0 for closed, 1 for open, or 2 for half-open.
func (m *CircuitBreaker) SetState(route string, state int) {
	m.state.WithLabelValues(route).Set(float64(state))
}

// RecordTransition records one circuit breaker state transition for route. The caller
// must supply fixed state names: closed, open, or half_open.
func (m *CircuitBreaker) RecordTransition(route, from, to string) {
	m.transitionsTotal.WithLabelValues(route, from, to).Inc()
}
