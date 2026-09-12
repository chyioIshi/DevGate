package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRateLimiterExportsRequestsByRouteAndOutcome(t *testing.T) {
	registry := prometheus.NewRegistry()
	rateLimiterMetrics := NewRateLimiter(registry)

	for range 2 {
		rateLimiterMetrics.RecordAllowedRequest("users")
	}
	rateLimiterMetrics.RecordRejectedRequest("users")
	rateLimiterMetrics.RecordAllowedRequest("orders")
	for range 3 {
		rateLimiterMetrics.RecordRejectedRequest("orders")
	}

	const want = `
# HELP devgate_rate_limiter_requests_total Total number of HTTP requests handled by the rate limiter.
# TYPE devgate_rate_limiter_requests_total counter
devgate_rate_limiter_requests_total{outcome="allowed",route="orders"} 1
devgate_rate_limiter_requests_total{outcome="allowed",route="users"} 2
devgate_rate_limiter_requests_total{outcome="rejected",route="orders"} 3
devgate_rate_limiter_requests_total{outcome="rejected",route="users"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(want)); err != nil {
		t.Errorf("registered rate limiter metrics mismatch: %v", err)
	}
}
