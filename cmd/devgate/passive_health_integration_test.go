package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestPassiveHealthCheckFailsClosedAfterEndpointEjection(t *testing.T) {
	t.Parallel()

	routeUpstream := testRouteUpstream(t, "http://upstream.example")
	routeUpstream.PassiveHealthCheck = &router.PassiveHealthCheckPolicy{
		ConsecutiveFailures: 1,
		EjectionDuration:    time.Hour,
	}
	route := router.Route{
		Name:       "users",
		Protocol:   router.ProtocolHTTP,
		PathPrefix: "/users",
		Upstream:   routeUpstream,
	}
	transport := &passiveFailureRoundTripper{}
	registry := prometheus.NewRegistry()
	passiveHealthMetrics := metrics.NewPassiveHealth(registry)
	runtime, err := routeRuntimeFromRoutes(
		[]router.Route{route},
		transport,
		nil,
		10,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		newTestActiveHealthCheckMetrics(),
		passiveHealthMetrics,
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("routeRuntimeFromRoutes() error = %v", err)
	}
	handler := runtime.handlers[route.Name]

	first := httptest.NewRecorder()
	handler.ServeHTTP(
		first,
		httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil),
	)
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"first response status = %d, want %d",
			first.Code,
			http.StatusServiceUnavailable,
		)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(
		second,
		httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil),
	)
	if second.Code != http.StatusServiceUnavailable {
		t.Errorf(
			"second response status = %d, want %d",
			second.Code,
			http.StatusServiceUnavailable,
		)
	}
	if transport.calls != 1 {
		t.Errorf("upstream RoundTrip() calls = %d, want 1", transport.calls)
	}

	const wantMetrics = `
# HELP devgate_passive_health_check_ejections_total Total number of passive upstream endpoint ejections.
# TYPE devgate_passive_health_check_ejections_total counter
devgate_passive_health_check_ejections_total{route="users",target="http://upstream.example"} 1
# HELP devgate_passive_health_check_endpoint_ejected Passive ejection state (1 for ejected, 0 for available).
# TYPE devgate_passive_health_check_endpoint_ejected gauge
devgate_passive_health_check_endpoint_ejected{route="users",target="http://upstream.example"} 1
`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(wantMetrics)); err != nil {
		t.Errorf("passive health check metrics mismatch: %v", err)
	}
}

func TestCircuitBreakerRejectionDoesNotEjectPassiveEndpoint(t *testing.T) {
	t.Parallel()

	routeUpstream := testRouteUpstream(t, "http://upstream.example")
	routeUpstream.PassiveHealthCheck = &router.PassiveHealthCheckPolicy{
		ConsecutiveFailures: 2,
		EjectionDuration:    time.Hour,
	}
	route := router.Route{
		Name:       "users",
		Protocol:   router.ProtocolHTTP,
		PathPrefix: "/users",
		Upstream:   routeUpstream,
	}
	transport := &passiveFailureRoundTripper{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	runtime, err := routeRuntimeFromRoutes(
		[]router.Route{route},
		transport,
		nil,
		1,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		newTestActiveHealthCheckMetrics(),
		newTestPassiveHealthMetrics(),
		nil,
		logger,
	)
	if err != nil {
		t.Fatalf("routeRuntimeFromRoutes() error = %v", err)
	}
	handler := runtime.handlers[route.Name]

	for range 2 {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil),
		)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf(
				"response status = %d, want %d",
				recorder.Code,
				http.StatusServiceUnavailable,
			)
		}
	}

	if transport.calls != 1 {
		t.Errorf("upstream RoundTrip() calls = %d, want 1", transport.calls)
	}
	if strings.Contains(logs.String(), "upstream endpoint passively ejected") {
		t.Error("circuit-breaker rejection caused passive endpoint ejection")
	}
}

type passiveFailureRoundTripper struct {
	calls int
}

func (t *passiveFailureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    request,
	}, nil
}
