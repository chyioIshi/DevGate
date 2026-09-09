package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCircuitBreakerMetricsTrackRouteLifecycle(t *testing.T) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, upstream.URL),
		},
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		upstream.Client().Transport,
		1,
		time.Minute,
		metrics.NewCircuitBreaker(registry),
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}
	handler := gateway.New(
		routeRouter,
		routeHandlers,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		firstRecorder,
		httptest.NewRequest(http.MethodGet, "/api/users/1", nil),
	)
	if firstRecorder.Code != http.StatusInternalServerError {
		t.Errorf("first response status = %d, want %d", firstRecorder.Code, http.StatusInternalServerError)
	}

	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		secondRecorder,
		httptest.NewRequest(http.MethodGet, "/api/users/2", nil),
	)
	if secondRecorder.Code != http.StatusServiceUnavailable {
		t.Errorf("second response status = %d, want %d", secondRecorder.Code, http.StatusServiceUnavailable)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}

	const want = `
# HELP devgate_circuit_breaker_state Current circuit breaker's state (0 = closed, 1 = open, 2 = half-open).
# TYPE devgate_circuit_breaker_state gauge
devgate_circuit_breaker_state{route="users"} 1
# HELP devgate_circuit_breaker_transitions_total Total number of state transitions of the circuit breaker.
# TYPE devgate_circuit_breaker_transitions_total counter
devgate_circuit_breaker_transitions_total{from="closed",route="users",to="open"} 1
# HELP devgate_circuit_breaker_rejected_requests_total Total number of requests rejected by the circuit breaker.
# TYPE devgate_circuit_breaker_rejected_requests_total counter
devgate_circuit_breaker_rejected_requests_total{route="users"} 1
`
	if err := testutil.GatherAndCompare(
		registry,
		strings.NewReader(want),
		"devgate_circuit_breaker_state",
		"devgate_circuit_breaker_transitions_total",
		"devgate_circuit_breaker_rejected_requests_total",
	); err != nil {
		t.Errorf("circuit breaker metrics mismatch: %v", err)
	}
}
