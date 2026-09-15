package requestbodylimit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewValidatesArguments(t *testing.T) {
	validHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name      string
		next      http.Handler
		maxBytes  int64
		wantError string
	}{
		{
			name:      "nil next handler",
			maxBytes:  1,
			wantError: "next handler must not be nil",
		},
		{
			name:      "zero limit",
			next:      validHandler,
			wantError: "max bytes must be positive",
		},
		{
			name:      "negative limit",
			next:      validHandler,
			maxBytes:  -1,
			wantError: "max bytes must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := New(test.next, test.maxBytes)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}
			if err.Error() != test.wantError {
				t.Errorf("New() error = %q, want %q", err, test.wantError)
			}
			if got != nil {
				t.Errorf("New() handler = %T, want nil", got)
			}
		})
	}
}

func TestNewLimitsRequestBody(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		maxBytes       int64
		wantBody       string
		wantLimitError bool
		unknownLength  bool
	}{
		{
			name:     "body below limit",
			body:     "data",
			maxBytes: 5,
			wantBody: "data",
		},
		{
			name:     "body exactly at limit",
			body:     "data",
			maxBytes: 4,
			wantBody: "data",
		},
		{
			name:           "body exceeds limit",
			body:           "data!",
			maxBytes:       4,
			wantBody:       "data",
			wantLimitError: true,
			unknownLength:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				gotBody  []byte
				gotError error
			)

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBody, gotError = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			})

			handler, err := New(next, test.maxBytes)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			if test.unknownLength {
				request.ContentLength = -1
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if string(gotBody) != test.wantBody {
				t.Errorf("downstream body = %q, want %q", gotBody, test.wantBody)
			}
			if response.Code != http.StatusNoContent {
				t.Errorf("status code = %d, want %d", response.Code, http.StatusNoContent)
			}

			var maxBytesError *http.MaxBytesError
			if test.wantLimitError {
				if !errors.As(gotError, &maxBytesError) {
					t.Fatalf("downstream error = %v, want *http.MaxBytesError", gotError)
				}
				if maxBytesError.Limit != test.maxBytes {
					t.Errorf("MaxBytesError.Limit = %d, want %d", maxBytesError.Limit, test.maxBytes)
				}
				return
			}

			if gotError != nil {
				t.Errorf("downstream error = %v, want nil", gotError)
			}
		})
	}
}

func TestNewRejectsKnownOversizedBodyBeforeReading(t *testing.T) {
	const maxBytes = 4

	body := &trackingReadCloser{}
	nextCalled := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		nextCalled = true
		_, _ = io.ReadAll(r.Body)
	})

	handler, err := New(next, maxBytes)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Body = body
	request.ContentLength = maxBytes + 1
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusRequestEntityTooLarge,
		)
	}
	wantResponseBody := http.StatusText(http.StatusRequestEntityTooLarge) + "\n"
	if response.Body.String() != wantResponseBody {
		t.Errorf("response body = %q, want %q", response.Body, wantResponseBody)
	}
	if nextCalled {
		t.Error("next handler was called")
	}
	if body.readCalls != 0 {
		t.Errorf("request body Read() calls = %d, want 0", body.readCalls)
	}
}

type trackingReadCloser struct {
	readCalls int
}

func (r *trackingReadCloser) Read([]byte) (int, error) {
	r.readCalls++
	return 0, io.EOF
}

func (*trackingReadCloser) Close() error {
	return nil
}
