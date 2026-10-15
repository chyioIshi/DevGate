package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	passiveHealthCheckSubsystem = "passive_health_check"

	passiveHealthEjectionsTotalMetricName  = "ejections_total"
	passiveHealthEndpointEjectedMetricName = "endpoint_ejected"
)

// PassiveHealth records passive upstream health metrics.
type PassiveHealth struct {
	ejectionsTotal  *prometheus.CounterVec
	endpointEjected *prometheus.GaugeVec
}

// NewPassiveHealth creates passive health metrics and registers their
// collectors with registerer. It panics if registration fails.
func NewPassiveHealth(registerer prometheus.Registerer) *PassiveHealth {
	ejectionsTotalCollectorOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: passiveHealthCheckSubsystem,
		Name:      passiveHealthEjectionsTotalMetricName,
		Help:      "Total number of passive upstream endpoint ejections.",
	}
	endpointEjectedCollectorOpts := prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: passiveHealthCheckSubsystem,
		Name:      passiveHealthEndpointEjectedMetricName,
		Help:      "Passive ejection state (1 for ejected, 0 for available).",
	}
	ejectionsTotalCollector := prometheus.NewCounterVec(
		ejectionsTotalCollectorOpts,
		[]string{"route", "target"},
	)
	endpointEjectedCollector := prometheus.NewGaugeVec(
		endpointEjectedCollectorOpts,
		[]string{"route", "target"},
	)
	registerer.MustRegister(
		ejectionsTotalCollector,
		endpointEjectedCollector,
	)
	return &PassiveHealth{
		ejectionsTotal:  ejectionsTotalCollector,
		endpointEjected: endpointEjectedCollector,
	}
}

// RecordEjection increments the ejection counter for an upstream endpoint.
func (p *PassiveHealth) RecordEjection(route, target string) {
	p.ejectionsTotal.WithLabelValues(route, target).Inc()
}

// SetEndpointEjected sets whether an upstream endpoint is currently ejected.
func (p *PassiveHealth) SetEndpointEjected(route, target string, ejected bool) {
	value := 0.0
	if ejected {
		value = 1.0
	}
	p.endpointEjected.WithLabelValues(route, target).Set(value)
}
