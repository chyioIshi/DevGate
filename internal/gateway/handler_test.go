package gateway_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/router"
)

func TestHandlerDispatchesToMatchedRoute(t *testing.T) {
	routeRouter := mustNewRouter(t, []router.Route{
		{
			Name:        "api",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	handler := gateway.New(
		routeRouter,
		map[string]http.Handler{
			"api": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, "api")
			}),
		},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if got, want := recorder.Body.String(), "api"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
}

func TestHandlerReturnsNotFoundWhenRouteDoesNotMatch(t *testing.T) {
	routeRouter := mustNewRouter(t, []router.Route{
		{
			Name:        "api",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	handlerCalled := false
	handler := gateway.New(
		routeRouter,
		map[string]http.Handler{
			"api": http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				handlerCalled = true
			}),
		},
	)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/orders", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if handlerCalled {
		t.Error("route handler was called for an unmatched path")
	}
}

func TestHandlerReturnsInternalServerErrorWhenRouteHandlerIsMissing(t *testing.T) {
	routeRouter := mustNewRouter(t, []router.Route{
		{
			Name:        "api",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	handler := gateway.New(routeRouter, nil)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}
}

func TestNewCopiesRouteHandlers(t *testing.T) {
	routeRouter := mustNewRouter(t, []router.Route{
		{
			Name:        "api",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	originalCalled := false
	replacementCalled := false
	routeHandlers := map[string]http.Handler{
		"api": http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			originalCalled = true
		}),
	}
	handler := gateway.New(routeRouter, routeHandlers)
	routeHandlers["api"] = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		replacementCalled = true
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	handler.ServeHTTP(recorder, request)

	if !originalCalled {
		t.Error("original route handler was not called")
	}
	if replacementCalled {
		t.Error("replacement route handler was called")
	}
}

func mustNewRouter(t *testing.T, routes []router.Route) *router.Router {
	t.Helper()

	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}

	return routeRouter
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", rawURL, err)
	}

	return parsedURL
}
