package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type receivedRequest struct {
	Path            string
	RawQuery        string
	Host            string
	XForwardedFor   string
	XForwardedHost  string
	XForwardedProto string
}

func TestReverseProxyForwardsRequest(t *testing.T) {
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
			}

			w.Header().Set("X-Upstream", "true")
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

	gateway := httptest.NewServer(New(targetURL, logger))
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

}

func TestReverseProxyReturnsBadGatewayWhenUpstreamIsUnavailable(t *testing.T) {
	var logBuffer bytes.Buffer
	var logRecord struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		Method  string `json:"method"`
		Path    string `json:"path"`
		Error   string `json:"error"`
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

	proxy := New(targetURL, logger)
	proxy.ServeHTTP(recorder, req)

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
	if logRecord.Error == "" {
		t.Error("log error is empty")
	}
}
