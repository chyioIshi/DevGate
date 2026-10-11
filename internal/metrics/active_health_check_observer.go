package metrics

import (
	"net/url"

	"github.com/chyioishi/devgate/internal/upstream"
)

// ActiveHealthCheckRouteMetrics records active health check metrics for one
// route.
type ActiveHealthCheckRouteMetrics struct {
	activeHealthCheck *ActiveHealthCheck
	route             string
}

var _ upstream.HealthCheckObserver = (*ActiveHealthCheckRouteMetrics)(nil)

// ForRoute binds active health check metrics to route and initializes all
// configured endpoints as healthy.
func (a *ActiveHealthCheck) ForRoute(
	route string,
	targets []url.URL,
) *ActiveHealthCheckRouteMetrics {
	for _, target := range targets {
		a.SetEndpointHealthy(route, target.Redacted(), true)
	}
	return &ActiveHealthCheckRouteMetrics{
		activeHealthCheck: a,
		route:             route,
	}
}

// RecordProbeResult records the outcome of one probe for the bound route.
func (a *ActiveHealthCheckRouteMetrics) RecordProbeResult(
	target url.URL,
	successful bool,
	_ error,
) {
	if successful {
		a.activeHealthCheck.RecordSuccessfulProbe(
			a.route,
			target.Redacted(),
		)
	} else {
		a.activeHealthCheck.RecordFailedProbe(
			a.route,
			target.Redacted(),
		)
	}
}

// RecordStateTransition updates the current endpoint state for the bound route.
func (a *ActiveHealthCheckRouteMetrics) RecordStateTransition(
	target url.URL,
	healthy bool,
) {
	a.activeHealthCheck.SetEndpointHealthy(
		a.route,
		target.Redacted(),
		healthy,
	)
}
