package main

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
)

func TestConfiguredRoutesDispatchToDifferentUpstreams(t *testing.T) {
	usersUpstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "users:"+r.URL.RequestURI())
		},
	))
	defer usersUpstream.Close()

	fallbackUpstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "fallback:"+r.URL.RequestURI())
		},
	))
	defer fallbackUpstream.Close()

	routeConfigs := []config.RouteConfig{
		{
			Name:        "fallback",
			Protocol:    "http",
			PathPrefix:  "/",
			UpstreamURL: fallbackUpstream.URL,
		},
		{
			Name:            "users",
			Protocol:        "http",
			PathPrefix:      "/api/users",
			UpstreamURL:     usersUpstream.URL,
			StripPathPrefix: true,
		},
	}

	routes, err := routesFromConfig(routeConfigs)
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "specific users route",
			target:     "/api/users/42?id=1",
			wantStatus: http.StatusCreated,
			wantBody:   "users:/42?id=1",
		},
		{
			name:       "fallback route",
			target:     "/orders?id=2",
			wantStatus: http.StatusAccepted,
			wantBody:   "fallback:/orders?id=2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			recorder := httptest.NewRecorder()

			gatewayHandler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Errorf("status code = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Body.String(); got != test.wantBody {
				t.Errorf("response body = %q, want %q", got, test.wantBody)
			}
		})
	}
}

func TestConfiguredUpstreamPoolDistributesRequestsRoundRobin(t *testing.T) {
	t.Parallel()

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "A")
	}))
	t.Cleanup(serverA.Close)
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "B")
	}))
	t.Cleanup(serverB.Close)

	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:       "users",
			Protocol:   "http",
			PathPrefix: "/users",
			Upstream: &config.UpstreamConfig{
				LoadBalancing: config.LoadBalancingPolicyRoundRobin,
				Discovery: &config.UpstreamDiscoveryConfig{
					Static: &config.StaticUpstreamDiscoveryConfig{
						Endpoints: []config.UpstreamEndpointConfig{
							{URL: serverA.URL},
							{URL: serverB.URL},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	for i, want := range []string{"A", "B", "A"} {
		request := httptest.NewRequest(http.MethodGet, "/users", nil)
		recorder := httptest.NewRecorder()

		gatewayHandler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Errorf("request %d status code = %d, want %d", i, recorder.Code, http.StatusOK)
		}
		if got := recorder.Body.String(); got != want {
			t.Errorf("request %d body = %q, want %q", i, got, want)
		}
	}
}

func TestConfiguredUpstreamPoolRoutesRequestsRandomly(t *testing.T) {
	t.Parallel()

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "A")
	}))
	t.Cleanup(serverA.Close)
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "B")
	}))
	t.Cleanup(serverB.Close)

	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:       "users",
			Protocol:   "http",
			PathPrefix: "/users",
			Upstream: &config.UpstreamConfig{
				LoadBalancing: config.LoadBalancingPolicyRandom,
				Discovery: &config.UpstreamDiscoveryConfig{
					Static: &config.StaticUpstreamDiscoveryConfig{
						Endpoints: []config.UpstreamEndpointConfig{
							{URL: serverA.URL},
							{URL: serverB.URL},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	for i := range 100 {
		request := httptest.NewRequest(http.MethodGet, "/users", nil)
		recorder := httptest.NewRecorder()

		gatewayHandler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status code = %d, want %d", i, recorder.Code, http.StatusOK)
		}
		if got := recorder.Body.String(); got != "A" && got != "B" {
			t.Fatalf("request %d body = %q, want response from a configured endpoint", i, got)
		}
	}
}

func TestConfiguredDirectResponseRouteReturnsWithoutUpstream(t *testing.T) {
	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:       "maintenance",
			PathPrefix: "/api",
			DirectResponse: &config.DirectResponseConfig{
				StatusCode: http.StatusServiceUnavailable,
				Body:       "service temporarily unavailable",
			},
			ResponseHeaders: &config.HeaderTransformConfig{
				Set: map[string]string{"X-Maintenance": "true"},
			},
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}

	transport := &countingRoundTripper{}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	recorder := httptest.NewRecorder()
	gatewayHandler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/users", nil),
	)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Body.String(); got != "service temporarily unavailable" {
		t.Errorf("response body = %q, want %q", got, "service temporarily unavailable")
	}
	if got := recorder.Header().Get("X-Maintenance"); got != "true" {
		t.Errorf("X-Maintenance = %q, want %q", got, "true")
	}
	if transport.calls != 0 {
		t.Errorf("transport calls = %d, want 0", transport.calls)
	}
}

func TestConfiguredRedirectRouteReturnsWithoutUpstream(t *testing.T) {
	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:      "legacy-api",
			PathExact: "/legacy",
			Redirect: &config.RedirectConfig{
				StatusCode: http.StatusPermanentRedirect,
				Location:   "/api/v2",
			},
			ResponseHeaders: &config.HeaderTransformConfig{
				Set: map[string]string{"Cache-Control": "no-store"},
			},
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}

	transport := &countingRoundTripper{}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	recorder := httptest.NewRecorder()
	gatewayHandler.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodGet, "/legacy", nil),
	)

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

func TestConfiguredExactRouteTakesPrecedenceOverPrefix(t *testing.T) {
	prefixUpstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "prefix")
		},
	))
	defer prefixUpstream.Close()

	exactUpstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "exact")
		},
	))
	defer exactUpstream.Close()

	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:        "prefix-users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: prefixUpstream.URL,
		},
		{
			Name:        "exact-users",
			Protocol:    "http",
			PathExact:   "/api/users",
			UpstreamURL: exactUpstream.URL,
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	tests := []struct {
		name     string
		path     string
		wantBody string
	}{
		{
			name:     "exact path uses exact route",
			path:     "/api/users",
			wantBody: "exact",
		},
		{
			name:     "child path uses prefix route",
			path:     "/api/users/42",
			wantBody: "prefix",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			recorder := httptest.NewRecorder()

			gatewayHandler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Errorf("status code = %d, want %d", recorder.Code, http.StatusOK)
			}
			if got := recorder.Body.String(); got != test.wantBody {
				t.Errorf("response body = %q, want %q", got, test.wantBody)
			}
		})
	}
}

func TestConfiguredHeaderRoutesDispatchToDifferentUpstreams(t *testing.T) {
	productionUpstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "production")
		},
	))
	defer productionUpstream.Close()

	stagingUpstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "staging")
		},
	))
	defer stagingUpstream.Close()

	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:        "production-users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			Methods:     []string{http.MethodGet},
			UpstreamURL: productionUpstream.URL,
			HeaderMatches: []config.HeaderMatchConfig{
				{Name: "X-Environment", Exact: "production"},
			},
		},
		{
			Name:        "staging-users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			Methods:     []string{http.MethodGet},
			UpstreamURL: stagingUpstream.URL,
			HeaderMatches: []config.HeaderMatchConfig{
				{Name: "X-Environment", Exact: "staging"},
			},
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
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
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		nil,
		discardLogger(),
		metrics.NewHTTP(registry),
	)

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "production route",
			header:     "production",
			wantStatus: http.StatusCreated,
			wantBody:   "production",
		},
		{
			name:       "staging route",
			header:     "staging",
			wantStatus: http.StatusAccepted,
			wantBody:   "staging",
		},
		{
			name:       "missing header",
			wantStatus: http.StatusNotFound,
			wantBody:   "Not Found\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)
			if test.header != "" {
				request.Header.Set("X-Environment", test.header)
			}
			recorder := httptest.NewRecorder()

			gatewayHandler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Errorf("status code = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Body.String(); got != test.wantBody {
				t.Errorf("response body = %q, want %q", got, test.wantBody)
			}
		})
	}
}

func TestRequestIDIsPropagatedThroughGateway(t *testing.T) {
	upstreamRequestIDCh := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			upstreamRequestIDCh <- r.Header.Get(requestid.HeaderName)
			w.WriteHeader(http.StatusNoContent)
		},
	))
	defer upstream.Close()

	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:        "upstream",
			Protocol:    "http",
			PathPrefix:  "/",
			UpstreamURL: upstream.URL,
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	logger := discardLogger()
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		metrics.NewCircuitBreaker(registry),
		metrics.NewRateLimiter(registry),
		nil,
		logger,
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}
	httpMetrics := metrics.NewHTTP(registry)
	gatewayHandler := gateway.New(routeRouter, routeHandlers, nil, logger, httpMetrics)
	handler := newHTTPMux(
		requestid.Middleware(gatewayHandler, nil, logger),
		metrics.Handler(registry),
		nil,
	)

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set(requestid.HeaderName, "spoofed-client-value")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	responseRequestID := recorder.Header().Get(requestid.HeaderName)
	if responseRequestID == "" {
		t.Fatal("response request ID is empty")
	}
	if responseRequestID == "spoofed-client-value" {
		t.Error("gateway preserved spoofed client request ID")
	}
	decodedRequestID, err := hex.DecodeString(responseRequestID)
	if err != nil {
		t.Fatalf("decode response request ID %q: %v", responseRequestID, err)
	}
	if len(decodedRequestID) != 16 {
		t.Errorf("decoded request ID length = %d, want %d", len(decodedRequestID), 16)
	}
	if upstreamRequestID := <-upstreamRequestIDCh; upstreamRequestID != responseRequestID {
		t.Errorf(
			"upstream request ID = %q, want response request ID %q",
			upstreamRequestID,
			responseRequestID,
		)
	}

	healthRequest := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRecorder := httptest.NewRecorder()

	handler.ServeHTTP(healthRecorder, healthRequest)

	if got := healthRecorder.Header().Get(requestid.HeaderName); got != "" {
		t.Errorf("health response request ID = %q, want empty value", got)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()

	handler.ServeHTTP(metricsRecorder, metricsRequest)

	if metricsRecorder.Code != http.StatusOK {
		t.Errorf("metrics status code = %d, want %d", metricsRecorder.Code, http.StatusOK)
	}
	for _, want := range []string{
		`devgate_http_requests_total{route="upstream",status="204"} 1`,
		`devgate_http_request_duration_seconds_count{route="upstream"} 1`,
		`devgate_http_requests_in_flight 0`,
	} {
		if !strings.Contains(metricsRecorder.Body.String(), want) {
			t.Errorf("metrics response body does not contain %q", want)
		}
	}
}
