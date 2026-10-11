package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	activeHealthCheckSubsystem = "active_health_check"

	activeHealthCheckProbesTotalMetricName     = "probes_total"
	activeHealthCheckEndpointHealthyMetricName = "endpoint_healthy"

	activeHealthCheckOutcomeSuccess = "success"
	activeHealthCheckOutcomeFailure = "failure"
)

// ActiveHealthCheck records active upstream health check metrics.
type ActiveHealthCheck struct {
	probesTotal     *prometheus.CounterVec
	endpointHealthy *prometheus.GaugeVec
}

// NewActiveHealthCheck creates active health check metrics and registers their
// collectors with registerer. It panics if registration fails.
func NewActiveHealthCheck(registerer prometheus.Registerer) *ActiveHealthCheck {
	probesTotalCollectorOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: activeHealthCheckSubsystem,
		Name:      activeHealthCheckProbesTotalMetricName,
		Help:      "Total number of active health check probes.",
	}

	endpointHealthyCollectorOpts := prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: activeHealthCheckSubsystem,
		Name:      activeHealthCheckEndpointHealthyMetricName,
		Help:      "Whether the endpoint is healthy (1 for healthy, 0 for unhealthy).",
	}

	probesTotalCollector := prometheus.NewCounterVec(
		probesTotalCollectorOpts,
		[]string{"route", "target", "outcome"},
	)
	endpointHealthyCollector := prometheus.NewGaugeVec(
		endpointHealthyCollectorOpts,
		[]string{"route", "target"},
	)

	registerer.MustRegister(
		probesTotalCollector,
		endpointHealthyCollector,
	)

	return &ActiveHealthCheck{
		probesTotal:     probesTotalCollector,
		endpointHealthy: endpointHealthyCollector,
	}
}

// RecordSuccessfulProbe records one successful probe for an upstream endpoint.
func (m *ActiveHealthCheck) RecordSuccessfulProbe(route, target string) {
	m.probesTotal.WithLabelValues(route, target, activeHealthCheckOutcomeSuccess).Inc()
}

// RecordFailedProbe records one failed probe for an upstream endpoint.
func (m *ActiveHealthCheck) RecordFailedProbe(route, target string) {
	m.probesTotal.WithLabelValues(route, target, activeHealthCheckOutcomeFailure).Inc()
}

// SetEndpointHealthy sets the current health state of an upstream endpoint.
func (m *ActiveHealthCheck) SetEndpointHealthy(route, target string, healthy bool) {
	value := 0.0
	if healthy {
		value = 1.0
	}
	m.endpointHealthy.WithLabelValues(route, target).Set(value)
}
