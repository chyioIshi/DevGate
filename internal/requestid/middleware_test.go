package requestid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewarePropagatesGeneratedRequestID(t *testing.T) {
	const generatedID = "00112233445566778899aabbccddeeff"
	type originalContextKey struct{}

	generateCalls := 0
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalls++

		requestID, ok := FromContext(r.Context())
		if !ok {
			t.Error("FromContext() ok = false, want true")
		}
		if requestID != generatedID {
			t.Errorf("FromContext() request ID = %q, want %q", requestID, generatedID)
		}
		if got := r.Header.Get(HeaderName); got != generatedID {
			t.Errorf("request header %s = %q, want %q", HeaderName, got, generatedID)
		}
		if got := r.Context().Value(originalContextKey{}); got != "preserved" {
			t.Errorf("original context value = %v, want %q", got, "preserved")
		}

		w.WriteHeader(http.StatusNoContent)
	})
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := middleware(next, logger, func() (string, error) {
		generateCalls++
		return generatedID, nil
	})

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	request.Header.Set(HeaderName, "spoofed-client-value")
	request = request.WithContext(context.WithValue(
		request.Context(),
		originalContextKey{},
		"preserved",
	))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if generateCalls != 1 {
		t.Errorf("generator calls = %d, want %d", generateCalls, 1)
	}
	if nextCalls != 1 {
		t.Errorf("next handler calls = %d, want %d", nextCalls, 1)
	}
	if recorder.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if got := recorder.Header().Get(HeaderName); got != generatedID {
		t.Errorf("response header %s = %q, want %q", HeaderName, got, generatedID)
	}
}

func TestMiddlewareReturnsInternalServerErrorWhenGenerationFails(t *testing.T) {
	errEntropyUnavailable := errors.New("entropy unavailable")
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logOutput, nil))
	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	})
	handler := middleware(next, logger, func() (string, error) {
		return "", errEntropyUnavailable
	})

	request := httptest.NewRequest(http.MethodGet, "/users", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if nextCalled {
		t.Error("next handler was called after request ID generation failure")
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}
	if got, want := recorder.Body.String(), http.StatusText(http.StatusInternalServerError)+"\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
	if got := recorder.Header().Get(HeaderName); got != "" {
		t.Errorf("response header %s = %q, want empty value", HeaderName, got)
	}

	var logRecord struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(logOutput.Bytes(), &logRecord); err != nil {
		t.Fatalf("decode error log %q: %v", logOutput.String(), err)
	}
	if logRecord.Level != slog.LevelError.String() {
		t.Errorf("log level = %q, want %q", logRecord.Level, slog.LevelError.String())
	}
	if logRecord.Message != "generate request ID" {
		t.Errorf("log message = %q, want %q", logRecord.Message, "generate request ID")
	}
	if !strings.Contains(logRecord.Error, errEntropyUnavailable.Error()) {
		t.Errorf("logged error = %q, want %q", logRecord.Error, errEntropyUnavailable)
	}
}

func TestFromContextReturnsFalseWhenRequestIDIsMissing(t *testing.T) {
	requestID, ok := FromContext(context.Background())
	if ok {
		t.Error("FromContext() ok = true, want false")
	}
	if requestID != "" {
		t.Errorf("FromContext() request ID = %q, want empty value", requestID)
	}
}
