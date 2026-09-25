package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
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
		nil,
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

func TestHandlersFromRoutesCreatesDirectResponseHandler(t *testing.T) {
	transport := &countingRoundTripper{}
	routes := []router.Route{
		{
			Name:       "maintenance",
			PathPrefix: "/api",
			DirectResponse: &router.DirectResponse{
				StatusCode: http.StatusServiceUnavailable,
				Body:       `{"status":"maintenance"}`,
			},
			ResponseHeaders: &router.HeaderTransformPolicy{
				Set: map[string]string{
					"Content-Type":      "application/json",
					"X-Direct-Response": "true",
				},
			},
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		transport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}

	handler := handlers["maintenance"]
	if handler == nil {
		t.Fatal("handler for route maintenance = nil, want non-nil handler")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://gateway.local/api", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Body.String(); got != `{"status":"maintenance"}` {
		t.Errorf("body = %q, want %q", got, `{"status":"maintenance"}`)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := recorder.Header().Get("X-Direct-Response"); got != "true" {
		t.Errorf("X-Direct-Response = %q, want %q", got, "true")
	}
	if transport.calls != 0 {
		t.Errorf("transport calls = %d, want 0", transport.calls)
	}
}

func TestHandlersFromRoutesAppliesRateLimitToDirectResponse(t *testing.T) {
	routes := []router.Route{
		{
			Name:       "maintenance",
			PathPrefix: "/api",
			DirectResponse: &router.DirectResponse{
				StatusCode: http.StatusServiceUnavailable,
			},
			RateLimit: &router.RateLimitPolicy{
				RequestsPerSecond: 1,
				Burst:             1,
			},
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		&countingRoundTripper{},
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}

	handler := handlers["maintenance"]
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "http://gateway.local/api", nil))
	if first.Code != http.StatusServiceUnavailable {
		t.Errorf("first status code = %d, want %d", first.Code, http.StatusServiceUnavailable)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://gateway.local/api", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second status code = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
}

func TestHandlersFromRoutesRejectsInvalidRouteActions(t *testing.T) {
	tests := []struct {
		name        string
		route       router.Route
		wantMessage string
	}{
		{
			name: "missing action",
			route: router.Route{
				Name:       "missing-action",
				PathPrefix: "/api",
			},
			wantMessage: "no upstream URL or direct response specified",
		},
		{
			name: "mutually exclusive actions",
			route: router.Route{
				Name:        "ambiguous-action",
				Protocol:    router.ProtocolHTTP,
				PathPrefix:  "/api",
				UpstreamURL: mustParseRouteURL(t, "http://api-service:8080"),
				DirectResponse: &router.DirectResponse{
					StatusCode: http.StatusServiceUnavailable,
				},
			},
			wantMessage: "upstream URL and direct response are mutually exclusive",
		},
		{
			name: "invalid direct response status",
			route: router.Route{
				Name:       "invalid-status",
				PathPrefix: "/api",
				DirectResponse: &router.DirectResponse{
					StatusCode: 199,
				},
			},
			wantMessage: "create direct response handler",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := handlersFromRoutes(
				[]router.Route{test.route},
				&countingRoundTripper{},
				testCircuitFailureThreshold,
				testCircuitOpenTimeout,
				newTestCircuitBreakerMetrics(),
				newTestRateLimiterMetrics(),
				nil,
				discardLogger(),
			)
			if err == nil {
				t.Fatalf("handlersFromRoutes() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("handlersFromRoutes() handlers = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("handlersFromRoutes() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestHandlersFromRoutesAppliesHeaderPolicies(t *testing.T) {
	routes := []router.Route{
		{
			Name:        "users",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api/users",
			UpstreamURL: mustParseRouteURL(t, "http://users-service:8080"),
			RequestHeaders: &router.HeaderTransformPolicy{
				Set:    map[string]string{"X-Gateway": "DevGate"},
				Remove: []string{"X-Legacy-Header"},
			},
			ResponseHeaders: &router.HeaderTransformPolicy{
				Set:    map[string]string{"X-Gateway-Response": "DevGate"},
				Remove: []string{"X-Legacy-Response-Header"},
			},
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		&http.Transport{},
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}

	reverseProxy, ok := handlers["users"].(*httputil.ReverseProxy)
	if !ok {
		t.Fatalf("handler type = %T, want *httputil.ReverseProxy", handlers["users"])
	}
	in := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
	in.Header.Set("X-Gateway", "client-controlled")
	in.Header.Set("X-Legacy-Header", "legacy")
	out := in.Clone(in.Context())

	reverseProxy.Rewrite(&httputil.ProxyRequest{In: in, Out: out})

	if got := out.Header.Values("X-Gateway"); len(got) != 1 || got[0] != "DevGate" {
		t.Errorf("outgoing X-Gateway values = %q, want %q", got, []string{"DevGate"})
	}
	if got := out.Header.Values("X-Legacy-Header"); len(got) != 0 {
		t.Errorf("outgoing X-Legacy-Header values = %q, want none", got)
	}

	response := &http.Response{Header: make(http.Header)}
	response.Header.Set("X-Gateway-Response", "upstream-controlled")
	response.Header.Set("X-Legacy-Response-Header", "legacy")
	if err := reverseProxy.ModifyResponse(response); err != nil {
		t.Fatalf("ModifyResponse() error = %v", err)
	}
	if got := response.Header.Values("X-Gateway-Response"); len(got) != 1 || got[0] != "DevGate" {
		t.Errorf("response X-Gateway-Response values = %q, want %q", got, []string{"DevGate"})
	}
	if got := response.Header.Values("X-Legacy-Response-Header"); len(got) != 0 {
		t.Errorf("response X-Legacy-Response-Header values = %q, want none", got)
	}
}

func TestHandlersFromRoutesPassesTrustedProxyCIDRsToReverseProxy(t *testing.T) {
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
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		[]netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}

	reverseProxy, ok := handlers["users"].(*httputil.ReverseProxy)
	if !ok {
		t.Fatalf("handler type = %T, want *httputil.ReverseProxy", handlers["users"])
	}
	in := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
	in.RemoteAddr = "10.0.0.2:1234"
	in.Header.Set("X-Forwarded-For", "203.0.113.10")
	out := in.Clone(in.Context())

	reverseProxy.Rewrite(&httputil.ProxyRequest{In: in, Out: out})

	const want = "203.0.113.10, 10.0.0.2"
	if got := out.Header.Get("X-Forwarded-For"); got != want {
		t.Errorf("outgoing X-Forwarded-For = %q, want %q", got, want)
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
		nil,
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
		nil,
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
		nil,
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
		nil,
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

func TestHandlersFromRoutesStripsPathPrefix(t *testing.T) {
	tests := []struct {
		name            string
		pathPrefix      string
		stripPathPrefix bool
		requestTarget   string
		wantEscapedPath string
		wantRawQuery    string
	}{
		{
			name:            "stripping disabled",
			pathPrefix:      "/api/users",
			requestTarget:   "http://gateway.local/api/users/42?id=1",
			wantEscapedPath: "/internal/api/users/42",
			wantRawQuery:    "id=1",
		},
		{
			name:            "nested escaped path",
			pathPrefix:      "/api/users",
			stripPathPrefix: true,
			requestTarget:   "http://gateway.local/api/users/a%2Fb?id=1",
			wantEscapedPath: "/internal/a%2Fb",
			wantRawQuery:    "id=1",
		},
		{
			name:            "exact prefix",
			pathPrefix:      "/api/users",
			stripPathPrefix: true,
			requestTarget:   "http://gateway.local/api/users",
			wantEscapedPath: "/internal/",
		},
		{
			name:            "root prefix is unchanged",
			pathPrefix:      "/",
			stripPathPrefix: true,
			requestTarget:   "http://gateway.local/orders/42",
			wantEscapedPath: "/internal/orders/42",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &recordingURLRoundTripper{}
			routes := []router.Route{
				{
					Name:            "users",
					Protocol:        router.ProtocolHTTP,
					PathPrefix:      test.pathPrefix,
					UpstreamURL:     mustParseRouteURL(t, "http://users-service:8080/internal"),
					StripPathPrefix: test.stripPathPrefix,
				},
			}

			handlers, err := handlersFromRoutes(
				routes,
				transport,
				testCircuitFailureThreshold,
				testCircuitOpenTimeout,
				newTestCircuitBreakerMetrics(),
				newTestRateLimiterMetrics(),
				nil,
				discardLogger(),
			)
			if err != nil {
				t.Fatalf("handlersFromRoutes() error = %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, test.requestTarget, nil)
			response := httptest.NewRecorder()
			handlers["users"].ServeHTTP(response, request)

			if response.Code != http.StatusNoContent {
				t.Errorf("status code = %d, want %d", response.Code, http.StatusNoContent)
			}
			if transport.calls != 1 {
				t.Errorf("upstream RoundTrip() calls = %d, want 1", transport.calls)
			}
			if transport.escapedPath != test.wantEscapedPath {
				t.Errorf(
					"upstream escaped path = %q, want %q",
					transport.escapedPath,
					test.wantEscapedPath,
				)
			}
			if transport.rawQuery != test.wantRawQuery {
				t.Errorf(
					"upstream raw query = %q, want %q",
					transport.rawQuery,
					test.wantRawQuery,
				)
			}
		})
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
		nil,
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

func TestHandlersFromRoutesAppliesRequestTimeout(t *testing.T) {
	transport := &contextBlockingRoundTripper{}
	routes := []router.Route{
		{
			Name:           "users",
			Protocol:       router.ProtocolHTTP,
			PathPrefix:     "/api/users",
			UpstreamURL:    mustParseRouteURL(t, "http://users-service:8080"),
			RequestTimeout: 10 * time.Millisecond,
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		transport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
	response := httptest.NewRecorder()
	handlers["users"].ServeHTTP(response, request)

	if response.Code != http.StatusGatewayTimeout {
		t.Errorf("status code = %d, want %d", response.Code, http.StatusGatewayTimeout)
	}
	if transport.calls != 1 {
		t.Errorf("upstream RoundTrip() calls = %d, want 1", transport.calls)
	}
}

func TestHandlersFromRoutesRejectsNegativeRequestTimeout(t *testing.T) {
	routes := []router.Route{
		{
			Name:           "users",
			Protocol:       router.ProtocolHTTP,
			PathPrefix:     "/api/users",
			UpstreamURL:    mustParseRouteURL(t, "http://users-service:8080"),
			RequestTimeout: -time.Nanosecond,
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err == nil {
		t.Fatal("handlersFromRoutes() error = nil, want request timeout configuration error")
	}
	if handlers != nil {
		t.Errorf("handlersFromRoutes() handlers = %+v, want nil", handlers)
	}
	if !strings.Contains(err.Error(), "users") {
		t.Errorf("handlersFromRoutes() error = %q, want route name", err)
	}
	if !strings.Contains(err.Error(), "request timeout") {
		t.Errorf("handlersFromRoutes() error = %q, want request timeout context", err)
	}
}

func TestHandlersFromRoutesAppliesRequestBodyLimit(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantBody      string
		wantStatus    int
		unknownLength bool
	}{
		{
			name:       "body at limit",
			body:       "data",
			wantBody:   "data",
			wantStatus: http.StatusNoContent,
		},
		{
			name:          "body exceeds limit",
			body:          "data!",
			wantBody:      "data",
			wantStatus:    http.StatusRequestEntityTooLarge,
			unknownLength: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &bodyReadingRoundTripper{}
			routes := []router.Route{
				{
					Name:                "users",
					Protocol:            router.ProtocolHTTP,
					PathPrefix:          "/api/users",
					UpstreamURL:         mustParseRouteURL(t, "http://users-service:8080"),
					MaxRequestBodyBytes: 4,
				},
			}

			handlers, err := handlersFromRoutes(
				routes,
				transport,
				testCircuitFailureThreshold,
				testCircuitOpenTimeout,
				newTestCircuitBreakerMetrics(),
				newTestRateLimiterMetrics(),
				nil,
				discardLogger(),
			)
			if err != nil {
				t.Fatalf("handlersFromRoutes() error = %v", err)
			}

			request := httptest.NewRequest(
				http.MethodPost,
				"http://gateway.local/api/users",
				strings.NewReader(test.body),
			)
			if test.unknownLength {
				request.ContentLength = -1
			}
			response := httptest.NewRecorder()
			handlers["users"].ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Errorf("status code = %d, want %d", response.Code, test.wantStatus)
			}
			if transport.calls != 1 {
				t.Errorf("upstream RoundTrip() calls = %d, want 1", transport.calls)
			}
			if string(transport.body) != test.wantBody {
				t.Errorf("upstream body = %q, want %q", transport.body, test.wantBody)
			}
		})
	}
}

func TestHandlersFromRoutesRejectsNegativeRequestBodyLimit(t *testing.T) {
	routes := []router.Route{
		{
			Name:                "users",
			Protocol:            router.ProtocolHTTP,
			PathPrefix:          "/api/users",
			UpstreamURL:         mustParseRouteURL(t, "http://users-service:8080"),
			MaxRequestBodyBytes: -1,
		},
	}

	handlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err == nil {
		t.Fatal("handlersFromRoutes() error = nil, want request body limit configuration error")
	}
	if handlers != nil {
		t.Errorf("handlersFromRoutes() handlers = %+v, want nil", handlers)
	}
	for _, context := range []string{"users", "request body limit"} {
		if !strings.Contains(err.Error(), context) {
			t.Errorf("handlersFromRoutes() error = %q, want context %q", err, context)
		}
	}
}

type countingRoundTripper struct {
	calls int
}

type bodyReadingRoundTripper struct {
	calls int
	body  []byte
}

func (t *bodyReadingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++

	body, err := io.ReadAll(request.Body)
	t.body = body
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       http.NoBody,
		Request:    request,
	}, nil
}

type contextBlockingRoundTripper struct {
	calls int
}

func (t *contextBlockingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case <-timer.C:
		return nil, errors.New("request context was not canceled")
	}
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

type recordingURLRoundTripper struct {
	calls       int
	escapedPath string
	rawQuery    string
}

func (t *recordingURLRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	t.escapedPath = request.URL.EscapedPath()
	t.rawQuery = request.URL.RawQuery
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
	return slog.New(slog.DiscardHandler)
}

func newTestCircuitBreakerMetrics() *metrics.CircuitBreaker {
	return metrics.NewCircuitBreaker(prometheus.NewRegistry())
}

func newTestRateLimiterMetrics() *metrics.RateLimiter {
	return metrics.NewRateLimiter(prometheus.NewRegistry())
}
