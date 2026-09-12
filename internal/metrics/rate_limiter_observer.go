package metrics

import "github.com/chyioishi/devgate/internal/ratelimit"

// RateLimiterRouteMetrics records rate limiter decisions for one route.
type RateLimiterRouteMetrics struct {
	rateLimiter *RateLimiter
	route       string
}

var _ ratelimit.Observer = (*RateLimiterRouteMetrics)(nil)

// ForRoute binds rate limiter metrics to route.
func (r *RateLimiter) ForRoute(route string) *RateLimiterRouteMetrics {
	return &RateLimiterRouteMetrics{
		rateLimiter: r,
		route:       route,
	}
}

// RecordAllowedRequest records one allowed request for the bound route.
func (r *RateLimiterRouteMetrics) RecordAllowedRequest() {
	r.rateLimiter.RecordAllowedRequest(r.route)
}

// RecordRejectedRequest records one rejected request for the bound route.
func (r *RateLimiterRouteMetrics) RecordRejectedRequest() {
	r.rateLimiter.RecordRejectedRequest(r.route)
}
