package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/requestid"
)

type receivedRequest struct {
	Path            string
	RawQuery        string
	Host            string
	XForwardedFor   string
	XForwardedHost  string
	XForwardedProto string
	RequestID       string
}

func TestReverseProxyForwardsRequest(t *testing.T) {
	const upstreamRequestID = "upstream-controlled-value"

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)
	receivedCh := make(chan receivedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			receivedCh <- receivedRequest{
				Path:            r.URL.Path,
				RawQuery:        r.URL.RawQuery,
				Host:            r.Host,
				XForwardedFor:   r.Header.Get("X-Forwarded-For"),
				XForwardedHost:  r.Header.Get("X-Forwarded-Host"),
				XForwardedProto: r.Header.Get("X-Forwarded-Proto"),
				RequestID:       r.Header.Get(requestid.HeaderName),
			}

			w.Header().Set("X-Upstream", "true")
			w.Header().Set(requestid.HeaderName, upstreamRequestID)
			w.WriteHeader(http.StatusCreated)

			if _, err := io.WriteString(w, "proxied"); err != nil {
				return
			}
		},
	))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	targetURL.Path = "/api"

	gateway := httptest.NewServer(requestid.Middleware(New(targetURL, http.DefaultTransport, logger), logger))
	defer gateway.Close()

	gatewayURL, err := url.Parse(gateway.URL)
	if err != nil {
		t.Fatalf("parse gateway URL: %v", err)
	}

	req, err := http.NewRequest(
		http.MethodGet,
		gateway.URL+"/users?id=1",
		nil,
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req.Header.Set("X-Forwarded-For", "123.123.123.123")
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set(requestid.HeaderName, "spoofed-client-value")

	resp, err := gateway.Client().Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if string(body) != "proxied" {
		t.Errorf("body = %q, want %q", string(body), "proxied")
	}

	got := <-receivedCh

	if got.Path != "/api/users" {
		t.Errorf("proxied request path = %q, want %q", got.Path, "/api/users")
	}
	if got.RawQuery != "id=1" {
		t.Errorf("proxied request raw query = %q, want %q", got.RawQuery, "id=1")
	}
	if got.Host != targetURL.Host {
		t.Errorf("proxied request host = %q, want %q", got.Host, targetURL.Host)
	}

	if got.XForwardedFor == "" {
		t.Error("proxied request X-Forwarded-For is empty")
	}

	if strings.Contains(got.XForwardedFor, "123.123.123.123") {
		t.Errorf("proxied request X-Forwarded-For contains spoofed address: %q", got.XForwardedFor)
	}
	if got.XForwardedHost != gatewayURL.Host {
		t.Errorf("proxied request X-Forwarded-Host = %q, want %q", got.XForwardedHost, gatewayURL.Host)
	}
	if got.XForwardedProto != gatewayURL.Scheme {
		t.Errorf("proxied request X-Forwarded-Proto = %q, want %q", got.XForwardedProto, gatewayURL.Scheme)
	}

	if got := resp.Header.Get("X-Upstream"); got != "true" {
		t.Errorf("response header X-Upstream = %q, want %q", got, "true")
	}
	responseRequestIDs := resp.Header.Values(requestid.HeaderName)
	if len(responseRequestIDs) != 1 {
		t.Fatalf("response request IDs = %q, want exactly one value", responseRequestIDs)
	}
	responseRequestID := responseRequestIDs[0]
	if responseRequestID == "" {
		t.Fatal("response request ID is empty")
	}
	if responseRequestID == upstreamRequestID {
		t.Error("upstream replaced gateway-generated response request ID")
	}
	if responseRequestID == "spoofed-client-value" {
		t.Error("gateway preserved spoofed client request ID")
	}
	if got.RequestID != responseRequestID {
		t.Errorf(
			"upstream request ID = %q, want response request ID %q",
			got.RequestID,
			responseRequestID,
		)
	}
}

func TestReverseProxyReturnsBadGatewayWhenUpstreamIsUnavailable(t *testing.T) {
	var logBuffer bytes.Buffer
	var logRecord struct {
		Level     string `json:"level"`
		Message   string `json:"msg"`
		Method    string `json:"method"`
		Path      string `json:"path"`
		RequestID string `json:"request_id"`
		Error     string `json:"error"`
	}
	logger := slog.New(
		slog.NewJSONHandler(&logBuffer, nil),
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"http://test.com/users?id=1",
		nil,
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	recorder := httptest.NewRecorder()

	proxy := New(targetURL, http.DefaultTransport, logger)
	handler := requestid.Middleware(proxy, logger)
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
	if string(body) != "Bad Gateway\n" {
		t.Errorf("body = %q, want %q", string(body), "Bad Gateway\n")
	}
	responseRequestID := resp.Header.Get(requestid.HeaderName)
	if responseRequestID == "" {
		t.Fatal("response request ID is empty")
	}
	if err := json.NewDecoder(&logBuffer).Decode(&logRecord); err != nil {
		t.Fatalf("decode json-log: %v", err)
	}
	if logRecord.Level != slog.LevelError.String() {
		t.Errorf("log level = %q, want %q", logRecord.Level, slog.LevelError.String())
	}
	if logRecord.Message != "proxy request failed" {
		t.Errorf("log msg = %q, want %q", logRecord.Message, "proxy request failed")
	}
	if logRecord.Method != http.MethodGet {
		t.Errorf("log method = %q, want %q", logRecord.Method, http.MethodGet)
	}
	if logRecord.Path != req.URL.Path {
		t.Errorf("log path = %q, want %q", logRecord.Path, req.URL.Path)
	}
	if logRecord.RequestID != responseRequestID {
		t.Errorf(
			"log request ID = %q, want response request ID %q",
			logRecord.RequestID,
			responseRequestID,
		)
	}
	if logRecord.Error == "" {
		t.Error("log error is empty")
	}
}

func TestStatusCodeForProxyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "ordinary error",
			err:  errors.New("connection refused"),
			want: http.StatusBadGateway,
		},
		{
			name: "open circuit",
			err:  ErrCircuitOpen,
			want: http.StatusServiceUnavailable,
		},
		{
			name: "wrapped open circuit",
			err:  fmt.Errorf("round trip: %w", ErrCircuitOpen),
			want: http.StatusServiceUnavailable,
		},
		{
			name: "open circuit takes precedence over timeout",
			err:  errors.Join(ErrCircuitOpen, testTimeoutError{timeout: true}),
			want: http.StatusServiceUnavailable,
		},
		{
			name: "reported timeout",
			err:  testTimeoutError{timeout: true},
			want: http.StatusGatewayTimeout,
		},
		{
			name: "reported non-timeout",
			err:  testTimeoutError{timeout: false},
			want: http.StatusBadGateway,
		},
		{
			name: "wrapped timeout",
			err:  fmt.Errorf("round trip: %w", testTimeoutError{timeout: true}),
			want: http.StatusGatewayTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := statusCodeForProxyError(test.err); got != test.want {
				t.Errorf("statusCodeForProxyError() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestReverseProxyReturnsServiceUnavailableWhenCircuitIsOpen(t *testing.T) {
	base := &recordingRoundTripper{err: errors.New("upstream unavailable")}
	circuitBreaker, err := NewCircuitBreakerTransport(base, 1, time.Minute)
	if err != nil {
		t.Fatalf("NewCircuitBreakerTransport() error = %v", err)
	}
	targetURL := &url.URL{Scheme: "http", Host: "upstream.local"}
	reverseProxy := New(
		targetURL,
		circuitBreaker,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	firstRequest := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	firstRecorder := httptest.NewRecorder()
	reverseProxy.ServeHTTP(firstRecorder, firstRequest)
	firstResponse := firstRecorder.Result()
	defer firstResponse.Body.Close()
	if firstResponse.StatusCode != http.StatusBadGateway {
		t.Errorf(
			"first response status = %d, want %d",
			firstResponse.StatusCode,
			http.StatusBadGateway,
		)
	}

	secondRequest := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	secondRecorder := httptest.NewRecorder()
	reverseProxy.ServeHTTP(secondRecorder, secondRequest)
	secondResponse := secondRecorder.Result()
	defer secondResponse.Body.Close()
	if secondResponse.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(
			"second response status = %d, want %d",
			secondResponse.StatusCode,
			http.StatusServiceUnavailable,
		)
	}
	body, err := io.ReadAll(secondResponse.Body)
	if err != nil {
		t.Fatalf("read second response body: %v", err)
	}
	if got, want := string(body), "Service Unavailable\n"; got != want {
		t.Errorf("second response body = %q, want %q", got, want)
	}
	if base.calls != 1 {
		t.Errorf("base RoundTrip() calls = %d, want 1", base.calls)
	}
}

func TestReverseProxyReturnsGatewayTimeoutWhenResponseHeadersAreLate(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-releaseUpstream
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	defer close(releaseUpstream)

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	transport, err := NewTransport(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	defer transport.CloseIdleConnections()

	proxy := New(targetURL, transport, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusGatewayTimeout {
		t.Errorf(
			"status code = %d, want %d",
			response.StatusCode,
			http.StatusGatewayTimeout,
		)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got, want := string(body), "Gateway Timeout\n"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
}

func TestReverseProxyRetriesGETAfterResponseHeaderTimeout(t *testing.T) {
	var attempts atomic.Int32
	releaseFirstAttempt := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			<-releaseFirstAttempt
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	defer close(releaseFirstAttempt)

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	baseTransport, err := NewTransport(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}
	defer baseTransport.CloseIdleConnections()
	retryTransport, err := NewRetryTransport(baseTransport, 2, time.Nanosecond)
	if err != nil {
		t.Fatalf("NewRetryTransport() error = %v", err)
	}

	proxy := New(
		targetURL,
		retryTransport,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("upstream attempts = %d, want 2", got)
	}
}

type testTimeoutError struct {
	timeout bool
}

func (e testTimeoutError) Error() string {
	return "test network error"
}

func (e testTimeoutError) Timeout() bool {
	return e.timeout
}
