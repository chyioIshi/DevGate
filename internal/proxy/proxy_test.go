package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"sync"
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
	const transformRequestID = "transform-controlled-value"

	logger := slog.New(
		slog.DiscardHandler,
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
			w.Header().Add("X-Replace", "first")
			w.Header().Add("X-Replace", "second")
			w.Header().Set("X-Remove", "remove me")
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

	responseHeaderTransform := func(header http.Header) {
		header.Del("X-Remove")
		header.Set("X-Replace", "replacement")
		header.Set("X-Added", "added")
		header.Set(requestid.HeaderName, transformRequestID)
	}
	gateway := httptest.NewServer(requestid.Middleware(
		New(targetURL, http.DefaultTransport, nil, responseHeaderTransform, nil, logger),
		logger,
	))
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
	defer func() {
		_ = resp.Body.Close()
	}()

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
	if got := resp.Header.Values("X-Replace"); len(got) != 1 || got[0] != "replacement" {
		t.Errorf("response X-Replace values = %q, want %q", got, []string{"replacement"})
	}
	if got := resp.Header.Values("X-Remove"); len(got) != 0 {
		t.Errorf("response X-Remove values = %q, want none", got)
	}
	if got := resp.Header.Get("X-Added"); got != "added" {
		t.Errorf("response X-Added = %q, want %q", got, "added")
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
	if responseRequestID == transformRequestID {
		t.Error("response transform replaced gateway-generated response request ID")
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

func TestReverseProxyStreamsResponseBody(t *testing.T) {
	const (
		firstChunk        = "first chunk\n"
		secondChunk       = "second chunk\n"
		testSignalTimeout = 5 * time.Second
	)

	firstChunkFlushed := make(chan struct{})
	releaseUpstream := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseUpstream)
		})
	}
	defer release()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		if _, err := io.WriteString(w, firstChunk); err != nil {
			return
		}
		flusher.Flush()
		close(firstChunkFlushed)

		select {
		case <-releaseUpstream:
		case <-r.Context().Done():
			return
		}

		_, _ = io.WriteString(w, secondChunk)
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	gateway := httptest.NewServer(New(
		targetURL,
		http.DefaultTransport,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	))
	defer gateway.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, gateway.URL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	type responseResult struct {
		response *http.Response
		err      error
	}
	responseCh := make(chan responseResult, 1)
	go func() {
		//nolint:bodyclose // Ownership is transferred through responseCh.
		response, requestErr := gateway.Client().Do(request)
		responseCh <- responseResult{response: response, err: requestErr}
	}()

	select {
	case <-firstChunkFlushed:
	case <-time.After(testSignalTimeout):
		t.Fatal("upstream did not flush the first response chunk")
	}

	var response *http.Response
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("send request: %v", result.err)
		}
		response = result.response
	case <-time.After(testSignalTimeout):
		t.Fatal("gateway did not forward response headers after upstream flushed first chunk")
	}
	defer func() {
		_ = response.Body.Close()
	}()

	firstReadCh := make(chan error, 1)
	firstRead := make([]byte, len(firstChunk))
	go func() {
		_, readErr := io.ReadFull(response.Body, firstRead)
		firstReadCh <- readErr
	}()

	select {
	case err := <-firstReadCh:
		if err != nil {
			t.Fatalf("read first response chunk: %v", err)
		}
	case <-time.After(testSignalTimeout):
		t.Fatal("gateway buffered the response body instead of forwarding the first chunk")
	}
	if got := string(firstRead); got != firstChunk {
		t.Errorf("first response chunk = %q, want %q", got, firstChunk)
	}

	release()
	rest, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read remaining response body: %v", err)
	}
	if got := string(rest); got != secondChunk {
		t.Errorf("remaining response body = %q, want %q", got, secondChunk)
	}
}

func TestReverseProxyForwardsResponseTrailers(t *testing.T) {
	const (
		body         = "proxied response body"
		trailerName  = "X-Upstream-Checksum"
		trailerValue = "sha256:abc123"
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", trailerName)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
		w.Header().Set(trailerName, trailerValue)
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	gateway := httptest.NewServer(New(
		targetURL,
		http.DefaultTransport,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	))
	defer gateway.Close()

	response, err := gateway.Client().Get(gateway.URL)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Errorf("response status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := string(responseBody); got != body {
		t.Errorf("response body = %q, want %q", got, body)
	}
	if got := response.Trailer.Get(trailerName); got != trailerValue {
		t.Errorf("response trailer %q = %q, want %q", trailerName, got, trailerValue)
	}
}

func TestReverseProxyStreamsRequestBody(t *testing.T) {
	const (
		firstChunk        = "first chunk\n"
		secondChunk       = "second chunk\n"
		testSignalTimeout = 5 * time.Second
	)

	firstChunkRead := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstRead := make([]byte, len(firstChunk))
		if _, err := io.ReadFull(r.Body, firstRead); err != nil {
			http.Error(w, "read first request chunk", http.StatusBadRequest)
			return
		}
		close(firstChunkRead)

		rest, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read remaining request body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(append(firstRead, rest...))
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	gateway := httptest.NewServer(New(
		targetURL,
		http.DefaultTransport,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	))
	defer gateway.Close()

	requestBody, requestBodyWriter := io.Pipe()
	defer func() {
		_ = requestBodyWriter.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		gateway.URL,
		requestBody,
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	type responseResult struct {
		response *http.Response
		err      error
	}
	responseCh := make(chan responseResult, 1)
	go func() {
		//nolint:bodyclose // Ownership is transferred through responseCh.
		response, requestErr := gateway.Client().Do(request)
		responseCh <- responseResult{response: response, err: requestErr}
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
		t.Fatal("client could not send the first request chunk")
	}

	select {
	case <-firstChunkRead:
	case <-time.After(testSignalTimeout):
		t.Fatal("gateway buffered the request body instead of forwarding the first chunk")
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
		t.Fatal("client could not finish the request body")
	}

	var response *http.Response
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatalf("send request: %v", result.err)
		}
		response = result.response
	case <-time.After(testSignalTimeout):
		t.Fatal("gateway did not return the upstream response")
	}
	defer func() {
		_ = response.Body.Close()
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Errorf("response status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got, want := string(body), firstChunk+secondChunk; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
}

func TestReverseProxyForwardsRequestTrailers(t *testing.T) {
	const (
		body         = "proxied request body"
		trailerName  = "X-Client-Checksum"
		trailerValue = "sha256:def456"
	)

	type receivedRequest struct {
		body    string
		trailer string
	}
	receivedCh := make(chan receivedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body", http.StatusBadRequest)
			return
		}
		receivedCh <- receivedRequest{
			body:    string(receivedBody),
			trailer: r.Trailer.Get(trailerName),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	targetURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	gateway := httptest.NewServer(New(
		targetURL,
		http.DefaultTransport,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	))
	defer gateway.Close()

	var request *http.Request
	requestBody := &eofCallbackReader{
		reader: strings.NewReader(body),
		onEOF: func() {
			request.Trailer.Set(trailerName, trailerValue)
		},
	}
	request, err = http.NewRequest(
		http.MethodPost,
		gateway.URL,
		io.NopCloser(requestBody),
	)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.ContentLength = -1
	request.Trailer = http.Header{trailerName: nil}

	response, err := gateway.Client().Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("response status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	received := <-receivedCh
	if received.body != body {
		t.Errorf("upstream request body = %q, want %q", received.body, body)
	}
	if received.trailer != trailerValue {
		t.Errorf("upstream request trailer %q = %q, want %q", trailerName, received.trailer, trailerValue)
	}
}

func TestReverseProxyTransformsOnlyOutgoingRequestHeaders(t *testing.T) {
	targetURL := &url.URL{Scheme: "http", Host: "upstream.local"}
	transform := func(header http.Header) {
		header.Del("X-Remove")
		header.Set("X-Replace", "replacement")
		header.Set("X-Added", "added")
		header.Set("X-Forwarded-Proto", "transform-controlled")
	}
	reverseProxy := New(
		targetURL,
		http.DefaultTransport,
		transform,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	)

	in := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	in.Header.Set("X-Remove", "remove me")
	in.Header.Add("X-Replace", "first")
	in.Header.Add("X-Replace", "second")
	out := in.Clone(in.Context())
	proxyRequest := &httputil.ProxyRequest{In: in, Out: out}

	reverseProxy.Rewrite(proxyRequest)

	if got := out.Header.Values("X-Replace"); len(got) != 1 || got[0] != "replacement" {
		t.Errorf("outgoing X-Replace values = %q, want %q", got, []string{"replacement"})
	}
	if got := out.Header.Values("X-Remove"); len(got) != 0 {
		t.Errorf("outgoing X-Remove values = %q, want none", got)
	}
	if got := out.Header.Get("X-Added"); got != "added" {
		t.Errorf("outgoing X-Added = %q, want %q", got, "added")
	}
	if got := out.Header.Get("X-Forwarded-Proto"); got != "http" {
		t.Errorf("outgoing X-Forwarded-Proto = %q, want gateway-controlled value %q", got, "http")
	}

	if got := in.Header.Values("X-Replace"); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("incoming X-Replace values = %q, want original values", got)
	}
	if got := in.Header.Get("X-Remove"); got != "remove me" {
		t.Errorf("incoming X-Remove = %q, want original value %q", got, "remove me")
	}
	if got := in.Header.Values("X-Added"); len(got) != 0 {
		t.Errorf("incoming X-Added values = %q, want none", got)
	}
}

func TestReverseProxyClonesTrustedProxyCIDRs(t *testing.T) {
	trustedCIDRs := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	reverseProxy := New(
		&url.URL{Scheme: "http", Host: "upstream.local"},
		http.DefaultTransport,
		nil,
		nil,
		trustedCIDRs,
		slog.New(slog.DiscardHandler),
	)

	trustedCIDRs[0] = netip.MustParsePrefix("192.0.2.0/24")

	in := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	in.RemoteAddr = "10.0.0.2:1234"
	in.Header.Set("X-Forwarded-For", "203.0.113.10")
	out := in.Clone(in.Context())

	reverseProxy.Rewrite(&httputil.ProxyRequest{In: in, Out: out})

	const want = "203.0.113.10, 10.0.0.2"
	if got := out.Header.Get("X-Forwarded-For"); got != want {
		t.Errorf("outgoing X-Forwarded-For = %q, want %q", got, want)
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

	proxy := New(targetURL, http.DefaultTransport, nil, nil, nil, logger)
	handler := requestid.Middleware(proxy, logger)
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	defer func() {
		_ = resp.Body.Close()
	}()
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
			name: "request body too large",
			err:  &http.MaxBytesError{Limit: 1024},
			want: http.StatusRequestEntityTooLarge,
		},
		{
			name: "wrapped request body too large",
			err:  fmt.Errorf("round trip: %w", &http.MaxBytesError{Limit: 1024}),
			want: http.StatusRequestEntityTooLarge,
		},
		{
			name: "request body too large takes precedence over timeout",
			err: errors.Join(
				&http.MaxBytesError{Limit: 1024},
				testTimeoutError{timeout: true},
			),
			want: http.StatusRequestEntityTooLarge,
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
	circuitBreaker, err := NewCircuitBreakerTransport(base, 1, time.Minute, noopCircuitBreakerObserver{})
	if err != nil {
		t.Fatalf("NewCircuitBreakerTransport() error = %v", err)
	}
	targetURL := &url.URL{Scheme: "http", Host: "upstream.local"}
	reverseProxy := New(
		targetURL,
		circuitBreaker,
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
	)

	firstRequest := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	firstRecorder := httptest.NewRecorder()
	reverseProxy.ServeHTTP(firstRecorder, firstRequest)
	firstResponse := firstRecorder.Result()
	defer func() {
		_ = firstResponse.Body.Close()
	}()
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
	defer func() {
		_ = secondResponse.Body.Close()
	}()
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

	proxy := New(targetURL, transport, nil, nil, nil, slog.New(slog.DiscardHandler))
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer func() {
		_ = response.Body.Close()
	}()
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
		nil,
		nil,
		nil,
		slog.New(slog.DiscardHandler),
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

type eofCallbackReader struct {
	reader io.Reader
	onEOF  func()
	once   sync.Once
}

func (r *eofCallbackReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) {
		r.once.Do(r.onEOF)
	}
	return n, err
}

func (e testTimeoutError) Error() string {
	return "test network error"
}

func (e testTimeoutError) Timeout() bool {
	return e.timeout
}
