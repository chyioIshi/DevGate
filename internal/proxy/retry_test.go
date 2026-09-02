package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var _ http.RoundTripper = (*RetryTransport)(nil)

const testRetryBaseDelay = 100 * time.Millisecond

func TestNewRetryTransportStoresConfiguration(t *testing.T) {
	base := testRoundTripper{}
	const maxAttempts = 3

	transport, err := NewRetryTransport(base, maxAttempts, testRetryBaseDelay)
	if err != nil {
		t.Fatalf("NewRetryTransport() error = %v", err)
	}
	if transport == nil {
		t.Fatal("NewRetryTransport() transport = nil, want transport")
	}
	if transport.base != base {
		t.Errorf("NewRetryTransport() base = %T, want original base %T", transport.base, base)
	}
	if transport.maxAttempts != maxAttempts {
		t.Errorf(
			"NewRetryTransport() maxAttempts = %d, want %d",
			transport.maxAttempts,
			maxAttempts,
		)
	}
	if transport.baseDelay != testRetryBaseDelay {
		t.Errorf(
			"NewRetryTransport() baseDelay = %s, want %s",
			transport.baseDelay,
			testRetryBaseDelay,
		)
	}
}

func TestNewRetryTransportRejectsNilBase(t *testing.T) {
	transport, err := NewRetryTransport(nil, 1, testRetryBaseDelay)
	if err == nil {
		t.Fatal("NewRetryTransport() error = nil, want validation error")
	}
	if transport != nil {
		t.Errorf("NewRetryTransport() transport = %v, want nil", transport)
	}
	if !strings.Contains(err.Error(), "base transport must not be nil") {
		t.Errorf("NewRetryTransport() error = %q, want base transport context", err)
	}
}

func TestNewRetryTransportRejectsNonPositiveMaxAttempts(t *testing.T) {
	for _, maxAttempts := range []int{0, -1} {
		transport, err := NewRetryTransport(testRoundTripper{}, maxAttempts, testRetryBaseDelay)
		if err == nil {
			t.Errorf(
				"NewRetryTransport(_, %d) error = nil, want validation error",
				maxAttempts,
			)
			continue
		}
		if transport != nil {
			t.Errorf(
				"NewRetryTransport(_, %d) transport = %v, want nil",
				maxAttempts,
				transport,
			)
		}
		if !strings.Contains(err.Error(), "max attempts must be positive") {
			t.Errorf("NewRetryTransport() error = %q, want max attempts context", err)
		}
	}
}

func TestNewRetryTransportRejectsNonPositiveBaseDelay(t *testing.T) {
	for _, baseDelay := range []time.Duration{0, -time.Millisecond} {
		t.Run(baseDelay.String(), func(t *testing.T) {
			transport, err := NewRetryTransport(testRoundTripper{}, 1, baseDelay)
			if err == nil {
				t.Fatal("NewRetryTransport() error = nil, want validation error")
			}
			if transport != nil {
				t.Errorf("NewRetryTransport() transport = %v, want nil", transport)
			}
			if !strings.Contains(err.Error(), "retry base delay must be positive") {
				t.Errorf("NewRetryTransport() error = %q, want retry base delay context", err)
			}
		})
	}
}

func TestRetryTransportRoundTripDelegatesToBase(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
		err      error
	}{
		{
			name:     "response",
			response: &http.Response{StatusCode: http.StatusNoContent},
		},
		{
			name: "error",
			err:  errors.New("round trip failed"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &recordingRoundTripper{
				response: test.response,
				err:      test.err,
			}
			transport, err := NewRetryTransport(base, 3, testRetryBaseDelay)
			if err != nil {
				t.Fatalf("NewRetryTransport() error = %v", err)
			}
			request := &http.Request{}

			response, err := transport.RoundTrip(request)

			if response != test.response {
				t.Errorf("RoundTrip() response = %p, want original response %p", response, test.response)
			}
			if err != test.err {
				t.Errorf("RoundTrip() error = %v, want original error %v", err, test.err)
			}
			if base.calls != 1 {
				t.Errorf("base RoundTrip() calls = %d, want 1", base.calls)
			}
			if base.request != request {
				t.Errorf("base request = %p, want original request %p", base.request, request)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	transportErr := errors.New("transport failed")
	tests := []struct {
		name            string
		method          string
		body            io.ReadCloser
		err             error
		cancelContext   bool
		wantShouldRetry bool
	}{
		{
			name:            "GET without body",
			method:          http.MethodGet,
			err:             transportErr,
			wantShouldRetry: true,
		},
		{
			name:            "HEAD with explicit empty body",
			method:          http.MethodHead,
			body:            http.NoBody,
			err:             transportErr,
			wantShouldRetry: true,
		},
		{
			name:   "no transport error",
			method: http.MethodGet,
		},
		{
			name:          "canceled request context",
			method:        http.MethodGet,
			err:           transportErr,
			cancelContext: true,
		},
		{
			name:   "POST without body",
			method: http.MethodPost,
			err:    transportErr,
		},
		{
			name:   "GET with body",
			method: http.MethodGet,
			body:   io.NopCloser(strings.NewReader("payload")),
			err:    transportErr,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cancelContext {
				canceledCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceledCtx
			}
			request := (&http.Request{
				Method: test.method,
				Body:   test.body,
			}).WithContext(ctx)

			if got := shouldRetry(request, test.err); got != test.wantShouldRetry {
				t.Errorf("shouldRetry() = %t, want %t", got, test.wantShouldRetry)
			}
		})
	}
}

func TestWaitForRetryReturnsAfterDelay(t *testing.T) {
	if err := waitForRetry(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("waitForRetry() error = %v, want nil", err)
	}
}

func TestWaitForRetryReturnsContextError(t *testing.T) {
	tests := []struct {
		name    string
		context func() context.Context
		wantErr error
	}{
		{
			name: "canceled",
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			context: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
				t.Cleanup(cancel)
				return ctx
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := waitForRetry(test.context(), time.Hour)
			if !errors.Is(err, test.wantErr) {
				t.Errorf("waitForRetry() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestRetryDelay(t *testing.T) {
	const baseDelay = 75 * time.Millisecond
	tests := []struct {
		retryIndex int
		want       time.Duration
	}{
		{retryIndex: 0, want: 75 * time.Millisecond},
		{retryIndex: 1, want: 150 * time.Millisecond},
		{retryIndex: 2, want: 300 * time.Millisecond},
		{retryIndex: 3, want: 600 * time.Millisecond},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("retry_%d", test.retryIndex+1), func(t *testing.T) {
			if got := retryDelay(baseDelay, test.retryIndex); got != test.want {
				t.Errorf(
					"retryDelay(%s, %d) = %s, want %s",
					baseDelay,
					test.retryIndex,
					got,
					test.want,
				)
			}
		})
	}
}

func TestJitterDelayStaysWithinRange(t *testing.T) {
	const maxDelay = 137 * time.Millisecond

	for range 1_000 {
		delay := jitterDelay(maxDelay)
		if delay < 0 || delay >= maxDelay {
			t.Fatalf("jitterDelay(%s) = %s, want value in [0, %s)", maxDelay, delay, maxDelay)
		}
	}
}

func TestJitterDelayWithOneNanosecondMaximumReturnsZero(t *testing.T) {
	if got := jitterDelay(time.Nanosecond); got != 0 {
		t.Errorf("jitterDelay(1ns) = %s, want 0s", got)
	}
}

func TestRetryTransportRoundTripAttempts(t *testing.T) {
	firstErr := errors.New("first attempt failed")
	secondErr := errors.New("second attempt failed")
	lastErr := errors.New("last attempt failed")
	successResponse := &http.Response{StatusCode: http.StatusNoContent}
	serviceUnavailableResponse := &http.Response{StatusCode: http.StatusServiceUnavailable}

	tests := []struct {
		name        string
		method      string
		maxAttempts int
		results     []roundTripResult
		wantCalls   int
		wantResp    *http.Response
		wantErr     error
	}{
		{
			name:        "succeeds after retry",
			method:      http.MethodGet,
			maxAttempts: 3,
			results: []roundTripResult{
				{err: firstErr},
				{response: successResponse},
			},
			wantCalls: 2,
			wantResp:  successResponse,
		},
		{
			name:        "exhausts maximum attempts",
			method:      http.MethodGet,
			maxAttempts: 3,
			results: []roundTripResult{
				{err: firstErr},
				{err: secondErr},
				{err: lastErr},
			},
			wantCalls: 3,
			wantErr:   lastErr,
		},
		{
			name:        "one attempt disables retries",
			method:      http.MethodGet,
			maxAttempts: 1,
			results: []roundTripResult{
				{err: firstErr},
			},
			wantCalls: 1,
			wantErr:   firstErr,
		},
		{
			name:        "unsafe method is not retried",
			method:      http.MethodPost,
			maxAttempts: 3,
			results: []roundTripResult{
				{err: firstErr},
			},
			wantCalls: 1,
			wantErr:   firstErr,
		},
		{
			name:        "upstream 5xx response is not retried",
			method:      http.MethodGet,
			maxAttempts: 3,
			results: []roundTripResult{
				{response: serviceUnavailableResponse},
			},
			wantCalls: 1,
			wantResp:  serviceUnavailableResponse,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &sequenceRoundTripper{results: test.results}
			transport, err := NewRetryTransport(base, test.maxAttempts, testRetryBaseDelay)
			if err != nil {
				t.Fatalf("NewRetryTransport() error = %v", err)
			}
			request := &http.Request{Method: test.method}

			response, err := transport.RoundTrip(request)

			if response != test.wantResp {
				t.Errorf("RoundTrip() response = %p, want %p", response, test.wantResp)
			}
			if err != test.wantErr {
				t.Errorf("RoundTrip() error = %v, want %v", err, test.wantErr)
			}
			if len(base.requests) != test.wantCalls {
				t.Errorf("base RoundTrip() calls = %d, want %d", len(base.requests), test.wantCalls)
			}
			for attempt, gotRequest := range base.requests {
				if gotRequest != request {
					t.Errorf("attempt %d request = %p, want original request %p", attempt+1, gotRequest, request)
				}
			}
		})
	}
}

type recordingRoundTripper struct {
	calls    int
	request  *http.Request
	response *http.Response
	err      error
}

func (t *recordingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	t.request = request
	return t.response, t.err
}

type roundTripResult struct {
	response *http.Response
	err      error
}

type sequenceRoundTripper struct {
	results  []roundTripResult
	requests []*http.Request
}

func (t *sequenceRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, request)
	result := t.results[len(t.requests)-1]
	return result.response, result.err
}
