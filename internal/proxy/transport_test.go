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
		MaxIdleConnsPerHost:   12,
		MaxConnsPerHost:       34,
		IdleConnTimeout:       45 * time.Second,
		TLSHandshakeTimeout:   7 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	replaceDefaultTransport(t, baseTransport)

	config := validTransportConfig()
	transport, err := NewTransport(config)
	if err != nil {
		t.Fatalf("NewTransport() error = %v", err)
	}

	if transport == baseTransport {
		t.Fatal("NewTransport() returned the default transport instead of a clone")
	}
	if got, want := transport.ResponseHeaderTimeout, config.ResponseHeaderTimeout; got != want {
		t.Errorf("ResponseHeaderTimeout = %s, want %s", got, want)
	}
	if got, want := baseTransport.ResponseHeaderTimeout, 30*time.Second; got != want {
		t.Errorf("default ResponseHeaderTimeout = %s, want unchanged value %s", got, want)
	}
	if transport.ForceAttemptHTTP2 != baseTransport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 was not preserved")
	}
	if got, want := transport.MaxIdleConns, config.MaxIdleConns; got != want {
		t.Errorf("MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := transport.MaxIdleConnsPerHost, config.MaxIdleConnsPerHost; got != want {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", got, want)
	}
	if got, want := transport.MaxConnsPerHost, config.MaxConnsPerHost; got != want {
		t.Errorf("MaxConnsPerHost = %d, want %d", got, want)
	}
	if got, want := transport.IdleConnTimeout, config.IdleConnTimeout; got != want {
		t.Errorf("IdleConnTimeout = %s, want %s", got, want)
	}
	if got, want := transport.TLSHandshakeTimeout, baseTransport.TLSHandshakeTimeout; got != want {
		t.Errorf("TLSHandshakeTimeout = %s, want %s", got, want)
	}
	if got, want := transport.ExpectContinueTimeout, baseTransport.ExpectContinueTimeout; got != want {
		t.Errorf("ExpectContinueTimeout = %s, want %s", got, want)
	}
}

func TestNewTransportRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		modify      func(*TransportConfig)
		wantMessage string
	}{
		{
			name: "max idle connections",
			modify: func(config *TransportConfig) {
				config.MaxIdleConns = 0
			},
			wantMessage: "upstream max idle connections must be positive",
		},
		{
			name: "max idle connections per host",
			modify: func(config *TransportConfig) {
				config.MaxIdleConnsPerHost = 0
			},
			wantMessage: "upstream max idle connections per host must be positive",
		},
		{
			name: "max connections per host",
			modify: func(config *TransportConfig) {
				config.MaxConnsPerHost = 0
			},
			wantMessage: "upstream max connections per host must be positive",
		},
		{
			name: "idle connection timeout",
			modify: func(config *TransportConfig) {
				config.IdleConnTimeout = 0
			},
			wantMessage: "upstream idle connection timeout must be positive",
		},
		{
			name: "response header timeout",
			modify: func(config *TransportConfig) {
				config.ResponseHeaderTimeout = 0
			},
			wantMessage: "upstream response header timeout must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validTransportConfig()
			test.modify(&config)

			transport, err := NewTransport(config)
			if err == nil {
				t.Fatal("NewTransport() error = nil, want validation error")
			}
			if transport != nil {
				t.Errorf("NewTransport() transport = %v, want nil", transport)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewTransport() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestNewTransportRejectsUnexpectedDefaultTransportType(t *testing.T) {
	replaceDefaultTransport(t, testRoundTripper{})

	transport, err := NewTransport(validTransportConfig())

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

func validTransportConfig() TransportConfig {
	return TransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
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
