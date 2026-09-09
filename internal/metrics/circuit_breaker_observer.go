package metrics

import "github.com/chyioishi/devgate/internal/proxy"

// CircuitBreakerRouteMetrics records circuit breaker metrics for one route.
type CircuitBreakerRouteMetrics struct {
	circuitBreaker *CircuitBreaker
	route          string
}

var _ proxy.CircuitBreakerObserver = (*CircuitBreakerRouteMetrics)(nil)

// ForRoute binds circuit breaker metrics to route and initializes its state as
// closed.
func (c *CircuitBreaker) ForRoute(
	route string,
) *CircuitBreakerRouteMetrics {
	c.SetState(route, int(proxy.CircuitStateClosed))
	return &CircuitBreakerRouteMetrics{
		circuitBreaker: c,
		route:          route,
	}
}

// RecordRejectedRequest records one request rejected for the bound route.
func (c *CircuitBreakerRouteMetrics) RecordRejectedRequest() {
	c.circuitBreaker.RecordRejectedRequest(c.route)
}

// RecordStateTransition records a state transition and updates the current
// state of the circuit breaker for the bound route.
func (c *CircuitBreakerRouteMetrics) RecordStateTransition(
	from,
	to proxy.CircuitState,
) {
	c.circuitBreaker.SetState(c.route, int(to))
	c.circuitBreaker.RecordTransition(
		c.route,
		from.String(),
		to.String(),
	)
}
