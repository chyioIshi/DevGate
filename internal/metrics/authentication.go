package metrics

import (
	"github.com/chyioishi/devgate/internal/identity"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	authenticationSubsystem = "authentication"

	authenticationRequestsTotalMetricName = "requests_total"

	authenticationOutcomeAuthenticated = "authenticated"
	authenticationOutcomeRejected      = "rejected"
)

// Authentication collects authentication outcomes by route and method.
type Authentication struct {
	authenticationRequestsTotal *prometheus.CounterVec
}

// NewAuthentication creates authentication metrics and registers their
// collectors with registerer. It panics if registration fails.
func NewAuthentication(registerer prometheus.Registerer) *Authentication {
	authenticationRequestsTotalCollectorOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: authenticationSubsystem,
		Name:      authenticationRequestsTotalMetricName,
		Help:      "Total number of HTTP requests by authentication method and outcome.",
	}

	authenticationRequestsTotalCollector := prometheus.NewCounterVec(
		authenticationRequestsTotalCollectorOpts,
		[]string{"route", "method", "outcome"},
	)

	registerer.MustRegister(authenticationRequestsTotalCollector)

	return &Authentication{
		authenticationRequestsTotal: authenticationRequestsTotalCollector,
	}
}

// RecordAuthenticatedRequest records one successfully authenticated request.
func (m *Authentication) RecordAuthenticatedRequest(route string, method identity.Method) {
	m.authenticationRequestsTotal.WithLabelValues(
		route,
		string(method),
		authenticationOutcomeAuthenticated,
	).Inc()
}

// RecordRejectedRequest records one request rejected by authentication.
func (m *Authentication) RecordRejectedRequest(route string, method identity.Method) {
	m.authenticationRequestsTotal.WithLabelValues(
		route,
		string(method),
		authenticationOutcomeRejected,
	).Inc()
}
