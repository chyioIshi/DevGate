package proxy

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewTransportClonesDefaultTransport(t *testing.T) {
	baseTransport := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          123,
		IdleConnTimeout:       45 * time.Second,
		TLSHandshakeTimeout:   7 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	replaceDefaultTransport(t, baseTransport)

	transport, err := NewTransport(5 * time.Second)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}

	if transport == baseTransport {
		t.Fatal("NewTransport() returned the default transport instead of a clone")
	}
	if got, want := transport.ResponseHeaderTimeout, 5*time.Second; got != want {
		t.Errorf("ResponseHeaderTimeout = %s, want %s", got, want)
	}
	if got, want := baseTransport.ResponseHeaderTimeout, 30*time.Second; got != want {
		t.Errorf("default ResponseHeaderTimeout = %s, want unchanged value %s", got, want)
	}
	if transport.ForceAttemptHTTP2 != baseTransport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 was not preserved")
	}
	if got, want := transport.MaxIdleConns, baseTransport.MaxIdleConns; got != want {
		t.Errorf("MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := transport.IdleConnTimeout, baseTransport.IdleConnTimeout; got != want {
		t.Errorf("IdleConnTimeout = %s, want %s", got, want)
	}
	if got, want := transport.TLSHandshakeTimeout, baseTransport.TLSHandshakeTimeout; got != want {
		t.Errorf("TLSHandshakeTimeout = %s, want %s", got, want)
	}
	if got, want := transport.ExpectContinueTimeout, baseTransport.ExpectContinueTimeout; got != want {
		t.Errorf("ExpectContinueTimeout = %s, want %s", got, want)
	}
}

func TestNewTransportRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		t.Run(timeout.String(), func(t *testing.T) {
			transport, err := NewTransport(timeout)
			if err == nil {
				t.Fatal("NewTransport() error = nil, want validation error")
			}
			if transport != nil {
				t.Errorf("NewTransport() transport = %v, want nil", transport)
			}
			if !strings.Contains(err.Error(), "upstream response header timeout must be positive") {
				t.Errorf("NewTransport() error = %q, want timeout validation context", err)
			}
		})
	}
}

func TestNewTransportRejectsUnexpectedDefaultTransportType(t *testing.T) {
	replaceDefaultTransport(t, testRoundTripper{})

	transport, err := NewTransport(time.Second)

	if err == nil {
		t.Fatal("NewTransport() error = nil, want unexpected type error")
	}
	if transport != nil {
		t.Errorf("NewTransport() transport = %v, want nil", transport)
	}
	if !strings.Contains(err.Error(), "testRoundTripper") {
		t.Errorf("NewTransport() error = %q, want actual transport type", err)
	}
}

type testRoundTripper struct{}

func (testRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func replaceDefaultTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()

	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = original
	})
}
