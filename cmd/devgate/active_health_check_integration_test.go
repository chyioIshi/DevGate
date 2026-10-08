package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/router"
)

func TestActiveHealthCheckFailsClosedWhenAllEndpointsAreUnhealthy(t *testing.T) {
	t.Parallel()

	routeUpstream := testRouteUpstream(t, "http://upstream.example")
	routeUpstream.ActiveHealthCheck = &router.ActiveHealthCheckPolicy{
		Path:                "/healthz",
		Interval:            time.Hour,
		Timeout:             time.Second,
		HealthyThreshold:    1,
		UnhealthyThreshold:  1,
		MaxConcurrentProbes: 1,
	}
	route := router.Route{
		Name:       "users",
		Protocol:   router.ProtocolHTTP,
		PathPrefix: "/users",
		Upstream:   routeUpstream,
	}
	dataTransport := &countingRoundTripper{}
	probeTransport := &failingProbeRoundTripper{
		called: make(chan struct{}, 1),
	}
	runtime, err := routeRuntimeFromRoutes(
		[]router.Route{route},
		dataTransport,
		&http.Client{Transport: probeTransport},
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("routeRuntimeFromRoutes() error = %v", err)
	}
	handler := runtime.handlers[route.Name]

	initialRecorder := httptest.NewRecorder()
	handler.ServeHTTP(
		initialRecorder,
		httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil),
	)
	if initialRecorder.Code != http.StatusNoContent {
		t.Fatalf(
			"initial response status = %d, want %d",
			initialRecorder.Code,
			http.StatusNoContent,
		)
	}

	stopHealthChecks := runtime.startHealthChecks(context.Background())
	defer stopHealthChecks()
	waitForProbeCall(t, probeTransport.called)

	waitForServiceUnavailable(t, handler)
	callsAfterFailure := dataTransport.calls
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil),
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusServiceUnavailable,
		)
	}
	if dataTransport.calls != callsAfterFailure {
		t.Errorf(
			"data-plane RoundTrip() calls = %d, want %d",
			dataTransport.calls,
			callsAfterFailure,
		)
	}
}

type failingProbeRoundTripper struct {
	called chan struct{}
}

func (t *failingProbeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	select {
	case t.called <- struct{}{}:
	default:
	}
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    request,
	}, nil
}

func waitForProbeCall(t *testing.T, called <-chan struct{}) {
	t.Helper()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("health probe was not sent")
	}
}

func waitForServiceUnavailable(t *testing.T, handler http.Handler) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil),
		)
		switch recorder.Code {
		case http.StatusServiceUnavailable:
			return
		case http.StatusNoContent:
			if time.Now().After(deadline) {
				t.Fatal("handler did not fail closed after unhealthy transition")
			}
			time.Sleep(time.Millisecond)
		default:
			t.Fatalf(
				"response status = %d, want %d or %d",
				recorder.Code,
				http.StatusNoContent,
				http.StatusServiceUnavailable,
			)
		}
	}
}
