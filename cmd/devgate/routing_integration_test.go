package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/router"
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
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: usersUpstream.URL,
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
	routeHandlers, err := handlersFromRoutes(routes, discardLogger())
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}
	gatewayHandler := gateway.New(routeRouter, routeHandlers)

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
			wantBody:   "users:/api/users/42?id=1",
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
