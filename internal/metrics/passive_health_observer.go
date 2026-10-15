package metrics

import (
	"net/url"
)

// PassiveHealthRouteMetrics records passive health metrics for one route.
type PassiveHealthRouteMetrics struct {
	passiveHealth *PassiveHealth
	route         string
}

// ForRoute binds passive health metrics to route and initializes all configured
// endpoints as available.
func (p *PassiveHealth) ForRoute(
	route string,
	targets []url.URL,
) *PassiveHealthRouteMetrics {
	for _, target := range targets {
		p.SetEndpointEjected(route, target.Redacted(), false)
	}
	return &PassiveHealthRouteMetrics{
		passiveHealth: p,
		route:         route,
	}
}

// RecordEjection records an ejection and marks the endpoint as ejected.
func (p *PassiveHealthRouteMetrics) RecordEjection(target url.URL) {
	p.passiveHealth.RecordEjection(p.route, target.Redacted())
	p.passiveHealth.SetEndpointEjected(p.route, target.Redacted(), true)
}

// RecordRecovery marks the endpoint as available again.
func (p *PassiveHealthRouteMetrics) RecordRecovery(target url.URL) {
	p.passiveHealth.SetEndpointEjected(p.route, target.Redacted(), false)
}
