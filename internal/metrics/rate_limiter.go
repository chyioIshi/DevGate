package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	rateLimiterSubsystem               = "rate_limiter"
	rateLimiterRequestsTotalMetricName = "requests_total"
	rateLimiterOutcomeAllowed          = "allowed"
	rateLimiterOutcomeRejected         = "rejected"
)

// RateLimiter collects rate limiter decisions by route and outcome.
type RateLimiter struct {
	requestsTotal *prometheus.CounterVec
}

// NewRateLimiter creates rate limiter metrics and registers their collectors
// with registerer. It panics if registration fails.
func NewRateLimiter(registerer prometheus.Registerer) *RateLimiter {
	requestsTotalCounterOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: rateLimiterSubsystem,
		Name:      rateLimiterRequestsTotalMetricName,
		Help:      "Total number of HTTP requests handled by the rate limiter.",
	}

	requestsTotalCollector := prometheus.NewCounterVec(
		requestsTotalCounterOpts,
		[]string{"route", "outcome"},
	)
	registerer.MustRegister(requestsTotalCollector)

	return &RateLimiter{
		requestsTotal: requestsTotalCollector,
	}
}

// RecordAllowedRequest records one request allowed for route.
func (m *RateLimiter) RecordAllowedRequest(route string) {
	m.requestsTotal.WithLabelValues(
		route,
		rateLimiterOutcomeAllowed,
	).Inc()
}

// RecordRejectedRequest records one request rejected for route.
func (m *RateLimiter) RecordRejectedRequest(route string) {
	m.requestsTotal.WithLabelValues(
		route,
		rateLimiterOutcomeRejected,
	).Inc()
}
