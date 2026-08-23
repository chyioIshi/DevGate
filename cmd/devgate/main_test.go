package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	mux := newHTTPMux(http.NotFoundHandler(), http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected Content-Type 'text/plain; charset=utf-8', got '%s'", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "ok\n" {
		t.Errorf("expected response body 'ok\\n', got '%s'", string(body))
	}
}

func TestHealthEndpointRejectsUnsupportedMethod(t *testing.T) {
	mux := newHTTPMux(http.NotFoundHandler(), http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status code %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}

	if resp.Header.Get("Allow") != "GET, HEAD" {
		t.Errorf("expected Allow header 'GET, HEAD', got '%s'", resp.Header.Get("Allow"))
	}
}

func TestMuxRoutesRequestsToProxy(t *testing.T) {
	path := "/users"
	wasCalled := false
	mux := newHTTPMux(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
				wasCalled = true
				if r.URL.Path != path {
					t.Errorf("expected request path %q, got %q", path, r.URL.Path)
				}
			},
		),
		http.NotFoundHandler(),
	)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status code %d, got %d", http.StatusNoContent, resp.StatusCode)
	}
	if !wasCalled {
		t.Errorf("expected proxy handler to be called")
	}
}

func TestMuxRoutesMetricsOutsideGateway(t *testing.T) {
	gatewayCalled := false
	metricsCalled := false
	mux := newHTTPMux(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			gatewayCalled = true
		}),
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			metricsCalled = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !metricsCalled {
		t.Error("metrics handler was not called")
	}
	if gatewayCalled {
		t.Error("gateway handler was called for the metrics endpoint")
	}
}

func TestMetricsEndpointRejectsUnsupportedMethod(t *testing.T) {
	metricsCalled := false
	mux := newHTTPMux(
		http.NotFoundHandler(),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			metricsCalled = true
		}),
	)
	request := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if got, want := recorder.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Errorf("Allow header = %q, want %q", got, want)
	}
	if metricsCalled {
		t.Error("metrics handler was called for an unsupported method")
	}
}
