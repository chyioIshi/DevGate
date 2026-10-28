package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	dnsDiscoverySubsystem = "dns_discovery"

	dnsDiscoveryRefreshesTotalMetricName    = "refreshes_total"
	dnsDiscoveryResolvedEndpointsMetricName = "resolved_endpoints"

	dnsDiscoveryOutcomeSuccess = "success"
	dnsDiscoveryOutcomeFailure = "failure"
)

// DNSDiscovery records DNS refresh outcomes and the size of the latest
// successfully applied endpoint snapshot.
type DNSDiscovery struct {
	refreshesTotal    *prometheus.CounterVec
	resolvedEndpoints *prometheus.GaugeVec
}

// NewDNSDiscovery creates DNS discovery metrics and registers their collectors
// with registerer. It panics if registration fails.
func NewDNSDiscovery(
	registerer prometheus.Registerer,
) *DNSDiscovery {
	refreshesTotalCollectorOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: dnsDiscoverySubsystem,
		Name:      dnsDiscoveryRefreshesTotalMetricName,
		Help:      "Total number of DNS discovery refresh attempts.",
	}
	resolvedEndpointsCollectorOpts := prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: dnsDiscoverySubsystem,
		Name:      dnsDiscoveryResolvedEndpointsMetricName,
		Help:      "Number of endpoints in the latest successful DNS discovery snapshot.",
	}
	refreshesTotalCollector := prometheus.NewCounterVec(
		refreshesTotalCollectorOpts,
		[]string{"route", "outcome"},
	)
	resolvedEndpointsCollector := prometheus.NewGaugeVec(
		resolvedEndpointsCollectorOpts,
		[]string{"route"},
	)

	registerer.MustRegister(
		refreshesTotalCollector,
		resolvedEndpointsCollector,
	)

	return &DNSDiscovery{
		refreshesTotal:    refreshesTotalCollector,
		resolvedEndpoints: resolvedEndpointsCollector,
	}
}

// RecordSuccessfulRefresh records a successful refresh and publishes the
// number of endpoints in the applied snapshot.
func (m *DNSDiscovery) RecordSuccessfulRefresh(route string, endpointCount int) {
	m.refreshesTotal.WithLabelValues(route, dnsDiscoveryOutcomeSuccess).Inc()
	m.resolvedEndpoints.WithLabelValues(route).Set(float64(endpointCount))
}

// RecordFailedRefresh records a failed refresh without changing the size of
// the latest successfully applied endpoint snapshot.
func (m *DNSDiscovery) RecordFailedRefresh(route string) {
	m.refreshesTotal.WithLabelValues(route, dnsDiscoveryOutcomeFailure).Inc()
}
