package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	testCircuitFailureThreshold = 3
	testCircuitOpenTimeout      = time.Minute
)

func handlersFromRoutes(
	routes []router.Route,
	transport http.RoundTripper,
	circuitFailureThreshold int,
	circuitOpenTimeout time.Duration,
	circuitBreakerMetrics *metrics.CircuitBreaker,
	rateLimiterMetrics *metrics.RateLimiter,
	trustedCIDRs []netip.Prefix,
	logger *slog.Logger,
) (map[string]http.Handler, error) {
	runtime, err := routeRuntimeFromRoutes(
		routes,
		transport,
		nil,
		circuitFailureThreshold,
		circuitOpenTimeout,
		circuitBreakerMetrics,
		rateLimiterMetrics,
		trustedCIDRs,
		logger,
	)
	if err != nil {
		return nil, err
	}
	return runtime.handlers, nil
}

func TestHandlersFromRoutesCreatesHTTPHandlers(t *testing.T) {
	transport := &countingRoundTripper{}
	routes := []router.Route{
		{
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
		},
		{
			Name:       "fallback",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/",
			Upstream:   testRouteUpstream(t, "http://frontend-service:8080"),
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
		request := httptest.NewRequest(http.MethodGet, "http://gateway.local/request", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Errorf("handler for route %q status = %d, want %d", route.Name, recorder.Code, http.StatusNoContent)
		}
		target := route.Upstream.Endpoints[0]
		outgoingRequest := transport.requests[len(transport.requests)-1]
		if outgoingRequest.URL.Scheme != target.Scheme ||
			outgoingRequest.URL.Host != target.Host {
			t.Errorf(
				"handler for route %q target = %q, want %q",
				route.Name,
				outgoingRequest.URL,
				&target,
			)
		}
	}

	if handlers["users"] == handlers["fallback"] {
		t.Error("different routes share the same handler")
	}
}

func TestRouteRuntimeFromRoutesCollectsActiveHealthCheckers(t *testing.T) {
	t.Parallel()

	activeUpstream := testRouteUpstream(t, "http://active.example")
	activeUpstream.ActiveHealthCheck = &router.ActiveHealthCheckPolicy{
		Path:                "/healthz",
		Interval:            time.Minute,
		Timeout:             time.Second,
		HealthyThreshold:    1,
		UnhealthyThreshold:  1,
		MaxConcurrentProbes: 1,
	}
	routes := []router.Route{
		{
			Name:       "active",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/active",
			Upstream:   activeUpstream,
		},
		{
			Name:       "passive",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/passive",
			Upstream:   testRouteUpstream(t, "http://passive.example"),
		},
	}

	runtime, err := routeRuntimeFromRoutes(
		routes,
		&countingRoundTripper{},
		&http.Client{Transport: &countingRoundTripper{}},
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
	if got := len(runtime.handlers); got != len(routes) {
		t.Errorf("handler count = %d, want %d", got, len(routes))
	}
	if got := len(runtime.healthCheckers); got != 1 {
		t.Errorf("health checker count = %d, want 1", got)
	}
}

func TestRouteRuntimeFromRoutesRequiresClientForActiveHealthCheck(t *testing.T) {
	t.Parallel()

	routeUpstream := testRouteUpstream(t, "http://active.example")
	routeUpstream.ActiveHealthCheck = &router.ActiveHealthCheckPolicy{
		Path:                "/healthz",
		Interval:            time.Minute,
		Timeout:             time.Second,
		HealthyThreshold:    1,
		UnhealthyThreshold:  1,
		MaxConcurrentProbes: 1,
	}
	route := router.Route{
		Name:       "active",
		Protocol:   router.ProtocolHTTP,
		PathPrefix: "/active",
		Upstream:   routeUpstream,
	}

	runtime, err := routeRuntimeFromRoutes(
		[]router.Route{route},
		&countingRoundTripper{},
		nil,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		newTestCircuitBreakerMetrics(),
		newTestRateLimiterMetrics(),
		nil,
		discardLogger(),
	)
	if err == nil {
		t.Fatal("routeRuntimeFromRoutes() error = nil, want health-check client error")
	}
	if runtime != nil {
		t.Errorf("routeRuntimeFromRoutes() runtime = %#v, want nil", runtime)
	}
	for _, context := range []string{
		"active",
		"create health checker",
		"http client was not provided",
	} {
		if !strings.Contains(err.Error(), context) {
			t.Errorf("routeRuntimeFromRoutes() error = %q, want context %q", err, context)
		}
	}
}

func TestHandlersFromRoutesCreatesRoundRobinHandler(t *testing.T) {
	t.Parallel()

	route := router.Route{
		Name:       "users",
		Protocol:   router.ProtocolHTTP,
		PathPrefix: "/api/users",
		Upstream: &router.Upstream{
			LoadBalancing: router.LoadBalancingPolicyRoundRobin,
			Endpoints: []url.URL{
				*mustParseRouteURL(t, "http://server-1:8080"),
				*mustParseRouteURL(t, "https://server-2:8443"),
			},
		},
	}

	transport := &countingRoundTripper{}
	handlers, err := handlersFromRoutes(
		[]router.Route{route},
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
	handler := handlers[route.Name]
	if handler == nil {
		t.Fatalf("handler for route %q is nil", route.Name)
	}

	wantTargets := []string{
		"http://server-1:8080/request",
		"https://server-2:8443/request",
		"http://server-1:8080/request",
	}
	for i, want := range wantTargets {
		request := httptest.NewRequest(http.MethodGet, "http://gateway.local/request", nil)
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if got := transport.requests[i].URL.String(); got != want {
			t.Errorf("request %d target = %q, want %q", i, got, want)
		}
	}
}

func TestHandlersFromRoutesCreatesRandomHandler(t *testing.T) {
	t.Parallel()

	route := router.Route{
		Name:       "users",
		Protocol:   router.ProtocolHTTP,
		PathPrefix: "/api/users",
		Upstream: &router.Upstream{
			LoadBalancing: router.LoadBalancingPolicyRandom,
			Endpoints: []url.URL{
				*mustParseRouteURL(t, "http://server-1:8080"),
				*mustParseRouteURL(t, "https://server-2:8443"),
			},
		},
	}

	transport := &countingRoundTripper{}
	handlers, err := handlersFromRoutes(
		[]router.Route{route},
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
	handler := handlers[route.Name]
	if handler == nil {
		t.Fatalf("handler for route %q is nil", route.Name)
	}

	wantTargets := map[string]struct{}{
		"http://server-1:8080/request":  {},
		"https://server-2:8443/request": {},
	}
	for i := range 100 {
		request := httptest.NewRequest(http.MethodGet, "http://gateway.local/request", nil)
		handler.ServeHTTP(httptest.NewRecorder(), request)
		got := transport.requests[i].URL.String()
		if _, exists := wantTargets[got]; !exists {
			t.Fatalf("request %d target = %q, want a configured endpoint", i, got)
		}
	}
}

func TestHandlersFromRoutesAppliesCustomProxyErrorResponse(t *testing.T) {
	routes := []router.Route{
		{
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
			ErrorResponses: map[int]router.ErrorResponse{
				http.StatusBadGateway: {
					Body: `{"error":"upstream unavailable"}`,
					Headers: map[string]string{
						"Content-Type":  "application/json",
						"Cache-Control": "no-store",
					},
				},
			},
		},
	}
	handlers, err := handlersFromRoutes(
		routes,
		&errorRoundTripper{err: errors.New("upstream unavailable")},
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

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
	handlers["users"].ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if got := recorder.Body.String(); got != `{"error":"upstream unavailable"}` {
		t.Errorf("body = %q, want %q", got, `{"error":"upstream unavailable"}`)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
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

func TestHandlersFromRoutesCreatesRedirectHandler(t *testing.T) {
	transport := &countingRoundTripper{}
	routes := []router.Route{
		{
			Name:      "legacy-api",
			PathExact: "/legacy",
			Redirect: &router.Redirect{
				StatusCode: http.StatusPermanentRedirect,
				Location:   "/api/v2",
			},
			ResponseHeaders: &router.HeaderTransformPolicy{
				Set: map[string]string{"Cache-Control": "no-store"},
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

	handler := handlers["legacy-api"]
	if handler == nil {
		t.Fatal("handler for route legacy-api = nil, want non-nil handler")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://gateway.local/legacy", nil))

	if recorder.Code != http.StatusPermanentRedirect {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusPermanentRedirect)
	}
	if got := recorder.Header().Get("Location"); got != "/api/v2" {
		t.Errorf("Location = %q, want %q", got, "/api/v2")
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
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
			ErrorResponses: map[int]router.ErrorResponse{
				http.StatusTooManyRequests: {
					Body:    `{"error":"rate limit exceeded"}`,
					Headers: map[string]string{"Content-Type": "application/json"},
				},
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
	if got := second.Body.String(); got != `{"error":"rate limit exceeded"}` {
		t.Errorf("second body = %q, want %q", got, `{"error":"rate limit exceeded"}`)
	}
	if got := second.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("second Content-Type = %q, want %q", got, "application/json")
	}
}

func TestHandlersFromRoutesAppliesRateLimitToRedirect(t *testing.T) {
	routes := []router.Route{
		{
			Name:      "legacy-api",
			PathExact: "/legacy",
			Redirect: &router.Redirect{
				StatusCode: http.StatusPermanentRedirect,
				Location:   "/api/v2",
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

	handler := handlers["legacy-api"]
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "http://gateway.local/legacy", nil))
	if first.Code != http.StatusPermanentRedirect {
		t.Errorf("first status code = %d, want %d", first.Code, http.StatusPermanentRedirect)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "http://gateway.local/legacy", nil))
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
			wantMessage: "no action configured for route",
		},
		{
			name: "mutually exclusive actions",
			route: router.Route{
				Name:       "ambiguous-action",
				Protocol:   router.ProtocolHTTP,
				PathPrefix: "/api",
				Upstream:   testRouteUpstream(t, "http://api-service:8080"),
				DirectResponse: &router.DirectResponse{
					StatusCode: http.StatusServiceUnavailable,
				},
			},
			wantMessage: "multiple actions configured for route",
		},
		{
			name: "empty upstream pool",
			route: router.Route{
				Name:       "empty-upstream",
				Protocol:   router.ProtocolHTTP,
				PathPrefix: "/api",
				Upstream: &router.Upstream{
					LoadBalancing: router.LoadBalancingPolicyRoundRobin,
				},
			},
			wantMessage: "create round-robin target picker",
		},
		{
			name: "unsupported load balancing policy",
			route: router.Route{
				Name:       "unsupported-policy",
				Protocol:   router.ProtocolHTTP,
				PathPrefix: "/api",
				Upstream: &router.Upstream{
					LoadBalancing: "least_connections",
					Endpoints: []url.URL{
						*mustParseRouteURL(t, "http://server-1:8080"),
					},
				},
			},
			wantMessage: `unsupported load balancing policy "least_connections"`,
		},
		{
			name: "upstream and redirect",
			route: router.Route{
				Name:       "ambiguous-action",
				Protocol:   router.ProtocolHTTP,
				PathPrefix: "/api",
				Upstream:   testRouteUpstream(t, "http://api-service:8080"),
				Redirect: &router.Redirect{
					StatusCode: http.StatusPermanentRedirect,
					Location:   "/api/v2",
				},
			},
			wantMessage: "multiple actions configured for route",
		},
		{
			name: "direct response and redirect",
			route: router.Route{
				Name:       "ambiguous-action",
				PathPrefix: "/api",
				DirectResponse: &router.DirectResponse{
					StatusCode: http.StatusServiceUnavailable,
				},
				Redirect: &router.Redirect{
					StatusCode: http.StatusPermanentRedirect,
					Location:   "/api/v2",
				},
			},
			wantMessage: "multiple actions configured for route",
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
		{
			name: "invalid redirect status",
			route: router.Route{
				Name:      "invalid-redirect",
				PathExact: "/legacy",
				Redirect: &router.Redirect{
					StatusCode: http.StatusOK,
					Location:   "/api/v2",
				},
			},
			wantMessage: "create redirect handler",
		},
		{
			name: "invalid custom error response status",
			route: router.Route{
				Name:       "invalid-error-response",
				Protocol:   router.ProtocolHTTP,
				PathPrefix: "/api",
				Upstream:   testRouteUpstream(t, "http://api-service:8080"),
				ErrorResponses: map[int]router.ErrorResponse{
					http.StatusOK: {},
				},
			},
			wantMessage: "create error responder for route",
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
	transport := &countingRoundTripper{
		responseHeader: http.Header{
			"X-Gateway-Response":       []string{"upstream-controlled"},
			"X-Legacy-Response-Header": []string{"legacy"},
		},
	}
	routes := []router.Route{
		{
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
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

	handler := handlers["users"]
	if handler == nil {
		t.Fatal("handler for route users is nil")
	}
	in := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
	in.Header.Set("X-Gateway", "client-controlled")
	in.Header.Set("X-Legacy-Header", "legacy")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, in)
	outHeader := transport.requests[0].Header

	if got := outHeader.Values("X-Gateway"); len(got) != 1 || got[0] != "DevGate" {
		t.Errorf("outgoing X-Gateway values = %q, want %q", got, []string{"DevGate"})
	}
	if got := outHeader.Values("X-Legacy-Header"); len(got) != 0 {
		t.Errorf("outgoing X-Legacy-Header values = %q, want none", got)
	}

	if got := recorder.Header().Values("X-Gateway-Response"); len(got) != 1 || got[0] != "DevGate" {
		t.Errorf("response X-Gateway-Response values = %q, want %q", got, []string{"DevGate"})
	}
	if got := recorder.Header().Values("X-Legacy-Response-Header"); len(got) != 0 {
		t.Errorf("response X-Legacy-Response-Header values = %q, want none", got)
	}
}

func TestHandlersFromRoutesPassesTrustedProxyCIDRsToReverseProxy(t *testing.T) {
	routes := []router.Route{
		{
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
		},
	}

	transport := &countingRoundTripper{}
	handlers, err := handlersFromRoutes(
		routes,
		transport,
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

	handler := handlers["users"]
	if handler == nil {
		t.Fatal("handler for route users is nil")
	}
	in := httptest.NewRequest(http.MethodGet, "http://gateway.local/api/users", nil)
	in.RemoteAddr = "10.0.0.2:1234"
	in.Header.Set("X-Forwarded-For", "203.0.113.10")
	handler.ServeHTTP(httptest.NewRecorder(), in)

	const want = "203.0.113.10, 10.0.0.2"
	if got := transport.requests[0].Header.Get("X-Forwarded-For"); got != want {
		t.Errorf("outgoing X-Forwarded-For = %q, want %q", got, want)
	}
}

func TestHandlersFromRoutesRejectsGRPCWithoutPartialResult(t *testing.T) {
	routes := []router.Route{
		{
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
		},
		{
			Name:       "greeter",
			Protocol:   router.ProtocolGRPC,
			PathPrefix: "/greeter.v1.Greeter",
			Upstream:   testRouteUpstream(t, "http://greeter-service:9090"),
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
			Name:       "websocket",
			Protocol:   router.Protocol("websocket"),
			PathPrefix: "/ws",
			Upstream:   testRouteUpstream(t, "http://websocket-service:8080"),
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
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
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
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
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
					Upstream:        testRouteUpstream(t, "http://users-service:8080/internal"),
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
			Name:       "users",
			Protocol:   router.ProtocolHTTP,
			PathPrefix: "/api/users",
			Upstream:   testRouteUpstream(t, "http://users-service:8080"),
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
			Upstream:       testRouteUpstream(t, "http://users-service:8080"),
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
			Upstream:       testRouteUpstream(t, "http://users-service:8080"),
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
		name              string
		body              string
		wantUpstreamBody  string
		wantResponseBody  string
		wantStatus        int
		wantUpstreamCalls int
		unknownLength     bool
	}{
		{
			name:              "body at limit",
			body:              "data",
			wantUpstreamBody:  "data",
			wantStatus:        http.StatusNoContent,
			wantUpstreamCalls: 1,
		},
		{
			name:             "known body length exceeds limit",
			body:             "data!",
			wantResponseBody: `{"error":"request body too large"}`,
			wantStatus:       http.StatusRequestEntityTooLarge,
		},
		{
			name:              "unknown body length exceeds limit",
			body:              "data!",
			wantUpstreamBody:  "data",
			wantResponseBody:  `{"error":"request body too large"}`,
			wantStatus:        http.StatusRequestEntityTooLarge,
			wantUpstreamCalls: 1,
			unknownLength:     true,
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
					Upstream:            testRouteUpstream(t, "http://users-service:8080"),
					MaxRequestBodyBytes: 4,
					ErrorResponses: map[int]router.ErrorResponse{
						http.StatusRequestEntityTooLarge: {
							Body: `{"error":"request body too large"}`,
							Headers: map[string]string{
								"Content-Type": "application/json",
							},
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
			if transport.calls != test.wantUpstreamCalls {
				t.Errorf(
					"upstream RoundTrip() calls = %d, want %d",
					transport.calls,
					test.wantUpstreamCalls,
				)
			}
			if string(transport.body) != test.wantUpstreamBody {
				t.Errorf("upstream body = %q, want %q", transport.body, test.wantUpstreamBody)
			}
			if got := response.Body.String(); got != test.wantResponseBody {
				t.Errorf("response body = %q, want %q", got, test.wantResponseBody)
			}
			if test.wantResponseBody != "" {
				if got := response.Header().Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q, want %q", got, "application/json")
				}
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
			Upstream:            testRouteUpstream(t, "http://users-service:8080"),
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
	calls          int
	requests       []*http.Request
	responseHeader http.Header
}

type errorRoundTripper struct {
	err error
}

func (t *errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
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
	t.requests = append(t.requests, request.Clone(request.Context()))
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     t.responseHeader.Clone(),
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

func testRouteUpstream(t *testing.T, rawURL string) *router.Upstream {
	t.Helper()

	return &router.Upstream{
		LoadBalancing: router.LoadBalancingPolicyRoundRobin,
		Endpoints:     []url.URL{*mustParseRouteURL(t, rawURL)},
	}
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
