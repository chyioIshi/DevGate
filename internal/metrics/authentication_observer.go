package metrics

import (
	"github.com/chyioishi/devgate/internal/apikey"
	"github.com/chyioishi/devgate/internal/identity"
)

// AuthenticationRouteMetrics records authentication outcomes for one route
// and method.
type AuthenticationRouteMetrics struct {
	authentication *Authentication
	route          string
	method         identity.Method
}

var _ apikey.Observer = (*AuthenticationRouteMetrics)(nil)

// ForRoute binds authentication metrics to route and method.
func (a *Authentication) ForRoute(
	route string,
	method identity.Method,
) *AuthenticationRouteMetrics {
	return &AuthenticationRouteMetrics{
		authentication: a,
		route:          route,
		method:         method,
	}
}

// RecordAuthenticatedRequest records one authenticated request for the bound
// route and method.
func (a *AuthenticationRouteMetrics) RecordAuthenticatedRequest() {
	a.authentication.RecordAuthenticatedRequest(a.route, a.method)
}

// RecordRejectedRequest records one rejected request for the bound route and
// method.
func (a *AuthenticationRouteMetrics) RecordRejectedRequest() {
	a.authentication.RecordRejectedRequest(a.route, a.method)
}
