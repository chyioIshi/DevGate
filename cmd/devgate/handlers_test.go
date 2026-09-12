package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	testCircuitFailureThreshold = 3
	testCircuitOpenTimeout      = time.Minute
)

func TestHandlersFromRoutesCreatesHTTPHandlers(t *testing.T) {
	transport := &http.Transport{}
	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, "http://users-service:8080"),
		},
		{
			Name:        "fallback",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/",
			UpstreamURL: mustParseRouteURL(t, "http://frontend-service:8080"),
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		transport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}
	if len(handlers) != len(routes) {
		t.Fatalf("handlersFromRoutes() handlers length = %d, want %d", len(handlers), len(routes))
	}

	circuitBreakers := make(map[string]*proxy.CircuitBreakerTransport, len(routes))
	for _, route := range routes {
		handler, exists := handlers[route.Name]
		if !exists {
			t.Errorf("handler for route %q does not exist", route.Name)
			continue
		}
		if handler == nil {
			t.Errorf("handler for route %q is nil", route.Name)
			continue
		}
		reverseProxy, ok := handler.(*httputil.ReverseProxy)
		if !ok {
			t.Errorf("handler for route %q has type %T, want *httputil.ReverseProxy", route.Name, handler)
			continue
		}
		circuitBreaker, ok := reverseProxy.Transport.(*proxy.CircuitBreakerTransport)
		if !ok {
			t.Errorf(
				"handler for route %q transport has type %T, want *proxy.CircuitBreakerTransport",
				route.Name,
				reverseProxy.Transport,
			)
			continue
		}
		circuitBreakers[route.Name] = circuitBreaker

		request := httptest.NewRequest(http.MethodGet, "http://gateway.local/request", nil)
		proxyRequest := &httputil.ProxyRequest{
			In:  request,
			Out: request.Clone(request.Context()),
		}
		reverseProxy.Rewrite(proxyRequest)
		if proxyRequest.Out.URL.Scheme != route.UpstreamURL.Scheme ||
			proxyRequest.Out.URL.Host != route.UpstreamURL.Host {
			t.Errorf(
				"handler for route %q target = %q, want %q",
				route.Name,
				proxyRequest.Out.URL,
				route.UpstreamURL,
			)
		}
	}

	if handlers["users"] == handlers["fallback"] {
		t.Error("different routes share the same handler")
	}
	if circuitBreakers["users"] == circuitBreakers["fallback"] {
		t.Error("different routes share the same circuit breaker")
	}
}

func TestHandlersFromRoutesRejectsGRPCWithoutPartialResult(t *testing.T) {
	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, "http://users-service:8080"),
		},
		{
			Name:        "greeter",
			Protocol:    router.ProtocolGRPC,
			PathPrefix:  "/greeter.v1.Greeter",
			UpstreamURL: mustParseRouteURL(t, "http://greeter-service:9090"),
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		discardLogger(),
	)
	if err == nil {
		t.Fatal("handlersFromRoutes() error = nil, want unsupported gRPC error")
	}
	if handlers != nil {
		t.Errorf("handlersFromRoutes() handlers = %+v, want nil", handlers)
	}
	if !strings.Contains(err.Error(), "greeter") {
		t.Errorf("handlersFromRoutes() error = %q, want route name", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "grpc") {
		t.Errorf("handlersFromRoutes() error = %q, want gRPC protocol context", err)
	}
}

func TestHandlersFromRoutesRejectsUnknownProtocol(t *testing.T) {
	routes := []router.Route{
		{
			Name:        "websocket",
			Protocol:    router.Protocol("websocket"),
			PathPrefix:  "/ws",
			UpstreamURL: mustParseRouteURL(t, "http://websocket-service:8080"),
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		discardLogger(),
	)
	if err == nil {
		t.Fatal("handlersFromRoutes() error = nil, want unsupported protocol error")
	}
	if handlers != nil {
		t.Errorf("handlersFromRoutes() handlers = %+v, want nil", handlers)
	}
	if !strings.Contains(err.Error(), "websocket") {
		t.Errorf("handlersFromRoutes() error = %q, want route and protocol context", err)
	}
}

func TestHandlersFromRoutesReturnsCircuitBreakerConfigurationError(t *testing.T) {
	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, "http://users-service:8080"),
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		0,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		discardLogger(),
	)
	if err == nil {
		t.Fatal("handlersFromRoutes() error = nil, want circuit breaker configuration error")
	}
	if handlers != nil {
		t.Errorf("handlersFromRoutes() handlers = %+v, want nil", handlers)
	}
	if !strings.Contains(err.Error(), "users") {
		t.Errorf("handlersFromRoutes() error = %q, want route name", err)
	}
	if !strings.Contains(err.Error(), "failure threshold") {
		t.Errorf("handlersFromRoutes() error = %q, want failure threshold context", err)
	}
}

func TestHandlersFromRoutesReturnsRateLimiterConfigurationError(t *testing.T) {
	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, "http://users-service:8080"),
			RateLimit: &router.RateLimitPolicy{
				RequestsPerSecond: 0,
				Burst:             1,
			},
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		discardLogger(),
	)
	if err == nil {
		t.Fatal("handlersFromRoutes() error = nil, want rate limiter configuration error")
	}
	if handlers != nil {
		t.Errorf("handlersFromRoutes() handlers = %+v, want nil", handlers)
	}
	if !strings.Contains(err.Error(), "users") {
		t.Errorf("handlersFromRoutes() error = %q, want route name", err)
	}
	if !strings.Contains(err.Error(), "rate limiter") {
		t.Errorf("handlersFromRoutes() error = %q, want rate limiter context", err)
	}
}

func TestHandlersFromRoutesAppliesRateLimitBeforeUpstream(t *testing.T) {
	transport := &countingRoundTripper{}
	registry := prometheus.NewRegistry()
	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, "http://users-service:8080"),
			RateLimit: &router.RateLimitPolicy{
				RequestsPerSecond: 1e-9,
				Burst:             2,
			},
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		transport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		metrics.NewCircuitBreaker(registry),
		metrics.NewRateLimiter(registry),
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}

	handler := handlers["users"]
	for requestNumber := 1; requestNumber <= 3; requestNumber++ {
		request := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		wantStatus := http.StatusNoContent
		if requestNumber == 3 {
			wantStatus = http.StatusTooManyRequests
		}
		if response.Code != wantStatus {
			t.Errorf(
				"request %d status code = %d, want %d",
				requestNumber,
				response.Code,
				wantStatus,
			)
		}
	}

	if transport.calls != 2 {
		t.Errorf("upstream RoundTrip() calls = %d, want 2", transport.calls)
	}

	const want = `
# HELP devgate_rate_limiter_requests_total Total number of HTTP requests handled by the rate limiter.
# TYPE devgate_rate_limiter_requests_total counter
devgate_rate_limiter_requests_total{outcome="allowed",route="users"} 2
devgate_rate_limiter_requests_total{outcome="rejected",route="users"} 1
`
	if err := testutil.GatherAndCompare(
		registry,
		strings.NewReader(want),
		"devgate_rate_limiter_requests_total",
	); err != nil {
		t.Errorf("rate limiter metrics mismatch: %v", err)
	}
}

type countingRoundTripper struct {
	calls int
}

func (t *countingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    request,
	}, nil
}

func mustParseRouteURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawURL, err)
	}

	return parsedURL
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestCircuitBreakerMetrics() *metrics.CircuitBreaker {
	return metrics.NewCircuitBreaker(prometheus.NewRegistry())
}

func newTestRateLimiterMetrics() *metrics.RateLimiter {
	return metrics.NewRateLimiter(prometheus.NewRegistry())
}
