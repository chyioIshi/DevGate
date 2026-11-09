package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type limiterFunc func(context.Context) (bool, error)

func (f limiterFunc) Allow(ctx context.Context) (bool, error) {
	return f(ctx)
}

type recordingObserver struct {
	allowed  int
	rejected int
	failed   int
}

func (o *recordingObserver) RecordAllowedRequest() {
	o.allowed++
}

func (o *recordingObserver) RecordRejectedRequest() {
	o.rejected++
}

func (o *recordingObserver) RecordFailedRequest() {
	o.failed++
}

func TestMiddlewareDelegatesAllowedRequest(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	var limiterCalls int
	var receivedRequest *http.Request
	observer := &recordingObserver{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequest = r
		w.WriteHeader(http.StatusNoContent)
	})
	handler := Middleware(next, limiterFunc(func(ctx context.Context) (bool, error) {
		limiterCalls++
		if ctx != request.Context() {
			t.Errorf("limiter context = %p, want request context %p", ctx, request.Context())
		}
		return true, nil
	}), observer, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if limiterCalls != 1 {
		t.Errorf("limiter calls = %d, want 1", limiterCalls)
	}
	if receivedRequest != request {
		t.Errorf("downstream request = %p, want original request %p", receivedRequest, request)
	}
	if observer.allowed != 1 {
		t.Errorf("allowed observations = %d, want 1", observer.allowed)
	}
	if observer.rejected != 0 {
		t.Errorf("rejected observations = %d, want 0", observer.rejected)
	}
	if observer.failed != 0 {
		t.Errorf("failed observations = %d, want 0", observer.failed)
	}
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestMiddlewareRejectsRequestWithoutCallingNext(t *testing.T) {
	var limiterCalls int
	var nextCalled bool
	observer := &recordingObserver{}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})
	handler := Middleware(next, limiterFunc(func(context.Context) (bool, error) {
		limiterCalls++
		return false, nil
	}), observer, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/users", nil)

	handler.ServeHTTP(recorder, request)

	if limiterCalls != 1 {
		t.Errorf("limiter calls = %d, want 1", limiterCalls)
	}
	if nextCalled {
		t.Error("downstream handler was called for rejected request")
	}
	if observer.allowed != 0 {
		t.Errorf("allowed observations = %d, want 0", observer.allowed)
	}
	if observer.rejected != 1 {
		t.Errorf("rejected observations = %d, want 1", observer.rejected)
	}
	if observer.failed != 0 {
		t.Errorf("failed observations = %d, want 0", observer.failed)
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

func TestMiddlewareDelegatesRejectedRequestToErrorResponder(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	observer := &recordingObserver{}
	var gotRequest *http.Request
	var gotStatus int
	calls := 0
	errorResponder := func(w http.ResponseWriter, r *http.Request, statusCode int) {
		calls++
		gotRequest = r
		gotStatus = statusCode
		w.Header().Set("X-Custom-Error", "true")
		w.WriteHeader(statusCode)
	}
	handler := Middleware(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("downstream handler was called for rejected request")
		}),
		limiterFunc(func(context.Context) (bool, error) { return false, nil }),
		observer,
		errorResponder,
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if calls != 1 {
		t.Errorf("error responder calls = %d, want 1", calls)
	}
	if gotRequest != request {
		t.Errorf("error responder request = %p, want %p", gotRequest, request)
	}
	if gotStatus != http.StatusTooManyRequests {
		t.Errorf("error responder status = %d, want %d", gotStatus, http.StatusTooManyRequests)
	}
	if observer.rejected != 1 {
		t.Errorf("rejected observations = %d, want 1", observer.rejected)
	}
	if got := recorder.Header().Get("X-Custom-Error"); got != "true" {
		t.Errorf("X-Custom-Error = %q, want %q", got, "true")
	}
}

func TestMiddlewareFailsClosedWhenLimiterReturnsError(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	observer := &recordingObserver{}
	var gotRequest *http.Request
	var gotStatus int
	errorResponder := func(w http.ResponseWriter, r *http.Request, statusCode int) {
		gotRequest = r
		gotStatus = statusCode
		w.WriteHeader(statusCode)
	}
	handler := Middleware(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("downstream handler was called after limiter failure")
		}),
		limiterFunc(func(context.Context) (bool, error) {
			return false, errors.New("redis unavailable")
		}),
		observer,
		errorResponder,
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if gotRequest != request {
		t.Errorf("error responder request = %p, want %p", gotRequest, request)
	}
	if gotStatus != http.StatusServiceUnavailable {
		t.Errorf("error responder status = %d, want %d", gotStatus, http.StatusServiceUnavailable)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if observer.allowed != 0 {
		t.Errorf("allowed observations = %d, want 0", observer.allowed)
	}
	if observer.rejected != 0 {
		t.Errorf("rejected observations = %d, want 0", observer.rejected)
	}
	if observer.failed != 1 {
		t.Errorf("failed observations = %d, want 1", observer.failed)
	}
}
