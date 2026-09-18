package requestbodylimit

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestNewDoesNotBufferRequestBody(t *testing.T) {
	const (
		firstChunk        = "first chunk\n"
		secondChunk       = "second chunk\n"
		testSignalTimeout = 5 * time.Second
	)

	firstChunkRead := make(chan struct{})
	var (
		gotBody []byte
		gotErr  error
	)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstRead := make([]byte, len(firstChunk))
		if _, gotErr = io.ReadFull(r.Body, firstRead); gotErr != nil {
			return
		}
		close(firstChunkRead)

		var rest []byte
		rest, gotErr = io.ReadAll(r.Body)
		gotBody = append(firstRead, rest...)
		w.WriteHeader(http.StatusNoContent)
	})
	handler, err := New(next, int64(len(firstChunk)+len(secondChunk)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	requestBody, requestBodyWriter := io.Pipe()
	defer func() {
		_ = requestBodyWriter.Close()
	}()
	request := httptest.NewRequest(http.MethodPost, "/", requestBody)
	request.ContentLength = -1
	response := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(response, request)
		close(handlerDone)
	}()

	firstWriteCh := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(requestBodyWriter, firstChunk)
		firstWriteCh <- writeErr
	}()
	select {
	case err := <-firstWriteCh:
		if err != nil {
			t.Fatalf("write first request chunk: %v", err)
		}
	case <-time.After(testSignalTimeout):
		t.Fatal("could not write the first request chunk")
	}

	select {
	case <-firstChunkRead:
	case <-handlerDone:
		t.Fatalf("downstream handler stopped before reading the first chunk: %v", gotErr)
	case <-time.After(testSignalTimeout):
		t.Fatal("request body limit buffered the request before calling the downstream handler")
	}

	remainingWriteCh := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(requestBodyWriter, secondChunk)
		if closeErr := requestBodyWriter.Close(); writeErr == nil {
			writeErr = closeErr
		}
		remainingWriteCh <- writeErr
	}()
	select {
	case err := <-remainingWriteCh:
		if err != nil {
			t.Fatalf("write remaining request body: %v", err)
		}
	case <-time.After(testSignalTimeout):
		t.Fatal("could not finish the request body")
	}

	select {
	case <-handlerDone:
	case <-time.After(testSignalTimeout):
		t.Fatal("request body limit did not finish after the request body was closed")
	}
	if gotErr != nil {
		t.Fatalf("downstream request body read error = %v", gotErr)
	}
	if got, want := string(gotBody), firstChunk+secondChunk; got != want {
		t.Errorf("downstream request body = %q, want %q", got, want)
	}
	if response.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", response.Code, http.StatusNoContent)
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
