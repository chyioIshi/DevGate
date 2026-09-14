package requesttimeout

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewValidatesArguments(t *testing.T) {
	validHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name      string
		next      http.Handler
		timeout   time.Duration
		wantError string
	}{
		{
			name:      "nil next handler",
			timeout:   time.Second,
			wantError: "next handler must not be nil",
		},
		{
			name:      "zero timeout",
			next:      validHandler,
			wantError: "timeout must be positive",
		},
		{
			name:      "negative timeout",
			next:      validHandler,
			timeout:   -time.Nanosecond,
			wantError: "timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.next, tt.timeout)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}
			if err.Error() != tt.wantError {
				t.Errorf("New() error = %q, want %q", err, tt.wantError)
			}
			if got != nil {
				t.Errorf("New() handler = %T, want nil", got)
			}
		})
	}
}

func TestNewAppliesTimeoutToRequestContext(t *testing.T) {
	type contextKey struct{}

	const contextValue = "request value"
	const timeout = time.Minute

	var (
		downstreamContext context.Context
		downstreamRequest *http.Request
	)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamContext = r.Context()
		downstreamRequest = r

		if got := r.Context().Value(contextKey{}); got != contextValue {
			t.Errorf("context value = %v, want %q", got, contextValue)
		}

		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("request context has no deadline")
		} else if remaining := time.Until(deadline); remaining <= 55*time.Second || remaining > timeout {
			t.Errorf("remaining timeout = %s, want within (55s, %s]", remaining, timeout)
		}

		w.WriteHeader(http.StatusNoContent)
	})

	handler, err := New(next, timeout)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	parentContext := context.WithValue(context.Background(), contextKey{}, contextValue)
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(parentContext)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if downstreamRequest == request {
		t.Error("downstream received the original request, want a request with the derived context")
	}
	if _, ok := request.Context().Deadline(); ok {
		t.Error("original request context has a deadline")
	}
	if downstreamContext == nil {
		t.Fatal("downstream context was not captured")
	}
	if !errors.Is(downstreamContext.Err(), context.Canceled) {
		t.Errorf("downstream context error = %v, want %v", downstreamContext.Err(), context.Canceled)
	}
}

func TestNewPreservesEarlierParentDeadline(t *testing.T) {
	parentContext, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	parentDeadline, ok := parentContext.Deadline()
	if !ok {
		t.Fatal("parent context has no deadline")
	}

	var downstreamDeadline time.Time
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var ok bool
		downstreamDeadline, ok = r.Context().Deadline()
		if !ok {
			t.Error("downstream context has no deadline")
		}
	})

	handler, err := New(next, 2*time.Minute)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(parentContext)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if !downstreamDeadline.Equal(parentDeadline) {
		t.Errorf("downstream deadline = %s, want parent deadline %s", downstreamDeadline, parentDeadline)
	}
}
