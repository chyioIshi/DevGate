package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type limiterFunc func() bool

func (f limiterFunc) Allow() bool {
	return f()
}

func TestMiddlewareDelegatesAllowedRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	var limiterCalls int
	var receivedRequest *http.Request
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequest = r
		w.WriteHeader(http.StatusNoContent)
	})
	handler := Middleware(next, limiterFunc(func() bool {
		limiterCalls++
		return true
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if limiterCalls != 1 {
		t.Errorf("limiter calls = %d, want 1", limiterCalls)
	}
	if receivedRequest != request {
		t.Errorf("downstream request = %p, want original request %p", receivedRequest, request)
	}
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestMiddlewareRejectsRequestWithoutCallingNext(t *testing.T) {
	var limiterCalls int
	var nextCalled bool
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})
	handler := Middleware(next, limiterFunc(func() bool {
		limiterCalls++
		return false
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users", nil)

	handler.ServeHTTP(recorder, request)

	if limiterCalls != 1 {
		t.Errorf("limiter calls = %d, want 1", limiterCalls)
	}
	if nextCalled {
		t.Error("downstream handler was called for rejected request")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if got, want := recorder.Body.String(), "Too Many Requests\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	if got, want := recorder.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}
