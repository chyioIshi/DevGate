package metrics

import (
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/identity"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestAuthenticationRouteMetricsRecordsBoundLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer := NewAuthentication(registry).ForRoute(
		"users",
		identity.MethodAPIKey,
	)

	observer.RecordAuthenticatedRequest()
	observer.RecordRejectedRequest()
	observer.RecordRejectedRequest()

	const want = `
# HELP devgate_authentication_requests_total Total number of HTTP requests by authentication method and outcome.
# TYPE devgate_authentication_requests_total counter
devgate_authentication_requests_total{method="api_key",outcome="authenticated",route="users"} 1
devgate_authentication_requests_total{method="api_key",outcome="rejected",route="users"} 2
`
	if err := testutil.GatherAndCompare(
		registry,
		strings.NewReader(want),
		"devgate_authentication_requests_total",
	); err != nil {
		t.Fatalf("authentication route metrics mismatch: %v", err)
	}
}
