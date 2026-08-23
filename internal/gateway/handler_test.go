package gateway_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
)

func TestHandlerDispatchesToMatchedRoute(t *testing.T) {
	logger, logOutput := newTestLogger()
	routeRouter := mustNewRouter(t, []router.Route{
		{
			Name:        "api",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	gatewayHandler := gateway.New(
		routeRouter,
		map[string]http.Handler{
			"api": http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, "api")
			}),
		},
		logger,
		metrics.NewHTTP(),
	)
	handler := requestid.Middleware(gatewayHandler, logger)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/users", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if got, want := recorder.Body.String(), "api"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	responseRequestID := recorder.Header().Get(requestid.HeaderName)
	if responseRequestID == "" {
		t.Fatal("response request ID is empty")
	}
	assertAccessLog(t, logOutput, accessLogRecord{
		Message:   "request completed",
		Method:    http.MethodGet,
		Path:      "/api/users",
		Route:     "api",
		RequestID: responseRequestID,
		Status:    http.StatusCreated,
		Bytes:     int64(recorder.Body.Len()),
	})
}

func TestHandlerReturnsNotFoundWhenRouteDoesNotMatch(t *testing.T) {
	logger, logOutput := newTestLogger()
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
		logger,
		metrics.NewHTTP(),
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
	assertAccessLog(t, logOutput, accessLogRecord{
		Message: "request completed",
		Method:  http.MethodGet,
		Path:    "/orders",
		Route:   "unmatched",
		Status:  http.StatusNotFound,
		Bytes:   int64(recorder.Body.Len()),
	})
}

func TestHandlerReturnsInternalServerErrorWhenRouteHandlerIsMissing(t *testing.T) {
	logger, logOutput := newTestLogger()
	routeRouter := mustNewRouter(t, []router.Route{
		{
			Name:        "api",
			Protocol:    router.ProtocolHTTP,
			PathPrefix:  "/api",
			UpstreamURL: mustParseURL(t, "http://api-service:8080"),
		},
	})
	handler := gateway.New(routeRouter, nil, logger, metrics.NewHTTP())

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
	assertAccessLog(t, logOutput, accessLogRecord{
		Message: "request completed",
		Method:  http.MethodGet,
		Path:    "/api/users",
		Route:   "api",
		Status:  http.StatusInternalServerError,
		Bytes:   int64(recorder.Body.Len()),
	})
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
	handler := gateway.New(routeRouter, routeHandlers, discardLogger(), metrics.NewHTTP())
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

type accessLogRecord struct {
	Message        string          `json:"msg"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	Route          string          `json:"route"`
	RequestID      string          `json:"request_id"`
	Status         int             `json:"status"`
	Bytes          int64           `json:"bytes"`
	DurationMS     *float64        `json:"duration_ms"`
	LegacyDuration json.RawMessage `json:"duration"`
}

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	return logger, &output
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func assertAccessLog(t *testing.T, output *bytes.Buffer, want accessLogRecord) {
	t.Helper()

	var got accessLogRecord
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal access log: %v", err)
	}

	if got.Message != want.Message {
		t.Errorf("access log message = %q, want %q", got.Message, want.Message)
	}
	if got.Method != want.Method {
		t.Errorf("access log method = %q, want %q", got.Method, want.Method)
	}
	if got.Path != want.Path {
		t.Errorf("access log path = %q, want %q", got.Path, want.Path)
	}
	if got.Route != want.Route {
		t.Errorf("access log route = %q, want %q", got.Route, want.Route)
	}
	if got.RequestID != want.RequestID {
		t.Errorf("access log request ID = %q, want %q", got.RequestID, want.RequestID)
	}
	if got.Status != want.Status {
		t.Errorf("access log status = %d, want %d", got.Status, want.Status)
	}
	if got.Bytes != want.Bytes {
		t.Errorf("access log bytes = %d, want %d", got.Bytes, want.Bytes)
	}
	if got.DurationMS == nil {
		t.Error("access log duration_ms is missing")
	} else if *got.DurationMS < 0 {
		t.Errorf("access log duration_ms = %f, want non-negative value", *got.DurationMS)
	}
	if len(got.LegacyDuration) != 0 {
		t.Errorf("legacy access log duration = %s, want field to be absent", got.LegacyDuration)
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
