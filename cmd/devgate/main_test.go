package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	var mux = newHTTPMux(http.NotFoundHandler())
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
	var mux = newHTTPMux(http.NotFoundHandler())
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
	mux := newHTTPMux(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
			wasCalled = true
			if r.URL.Path != path {
				t.Errorf("expected request path %q, got %q", path, r.URL.Path)
			}
		},
	))

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
