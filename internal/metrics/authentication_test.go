package metrics

import (
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/identity"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestAuthenticationExportsRequestsByRouteMethodAndOutcome(t *testing.T) {
	registry := prometheus.NewRegistry()
	authentication := NewAuthentication(registry)

	authentication.RecordAuthenticatedRequest("orders", identity.MethodAPIKey)
	authentication.RecordAuthenticatedRequest("orders", identity.MethodAPIKey)
	authentication.RecordRejectedRequest("orders", identity.MethodAPIKey)
	authentication.RecordRejectedRequest("admin", identity.MethodAPIKey)

	const want = `
# HELP devgate_authentication_requests_total Total number of HTTP requests by authentication method and outcome.
# TYPE devgate_authentication_requests_total counter
devgate_authentication_requests_total{method="api_key",outcome="authenticated",route="orders"} 2
devgate_authentication_requests_total{method="api_key",outcome="rejected",route="admin"} 1
devgate_authentication_requests_total{method="api_key",outcome="rejected",route="orders"} 1
`
	if err := testutil.GatherAndCompare(
		registry,
		strings.NewReader(want),
		"devgate_authentication_requests_total",
	); err != nil {
		t.Fatalf("authentication metrics mismatch: %v", err)
	}
}
