package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

const tlsIntegrationTimeout = 5 * time.Second

func TestServeUsesPlainHTTPWhenTLSIsDisabled(t *testing.T) {
	server := &http.Server{
		Addr: reserveTestAddress(t),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	response := requestRunningServer(t, server, http.DefaultClient, "http://"+server.Addr)
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusNoContent {
		t.Errorf("response status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if response.TLS != nil {
		t.Error("plain HTTP response unexpectedly contains TLS state")
	}
}

func TestServePerformsTLSHandshakeWhenTLSIsConfigured(t *testing.T) {
	certFile, keyFile := writeTestCertificatePair(t)
	serverTLSConfig, err := loadServerTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("loadServerTLSConfig() error = %v", err)
	}
	server := &http.Server{
		Addr:      reserveTestAddress(t),
		TLSConfig: serverTLSConfig,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	certificatePEM, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("read test certificate: %v", err)
	}
	rootCAs := x509.NewCertPool()
	if ok := rootCAs.AppendCertsFromPEM(certificatePEM); !ok {
		t.Fatal("add test certificate to root pool")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}

	_, port, err := net.SplitHostPort(server.Addr)
	if err != nil {
		t.Fatalf("split test server address: %v", err)
	}
	response := requestRunningServer(
		t,
		server,
		client,
		"https://localhost:"+port,
	)
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusNoContent {
		t.Errorf("response status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if response.TLS == nil {
		t.Fatal("HTTPS response TLS state = nil")
	}
	if response.TLS.Version < tls.VersionTLS12 {
		t.Errorf("negotiated TLS version = %#x, want TLS 1.2 or newer", response.TLS.Version)
	}
	if response.ProtoMajor != 2 {
		t.Errorf("negotiated HTTP version = %q, want HTTP/2", response.Proto)
	}
}

func requestRunningServer(
	t *testing.T,
	server *http.Server,
	client *http.Client,
	requestURL string,
) *http.Response {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- serve(
			ctx,
			server,
			slog.New(slog.DiscardHandler),
			time.Second,
		)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-serveResult:
			if err != nil {
				t.Errorf("serve() error = %v", err)
			}
		case <-time.After(tlsIntegrationTimeout):
			t.Error("serve() did not stop after context cancellation")
		}
	})

	deadline := time.Now().Add(tlsIntegrationTimeout)
	for {
		response, err := client.Get(requestURL)
		if err == nil {
			return response
		}
		if time.Now().After(deadline) {
			t.Fatalf("request test server: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func reserveTestAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release test address %s: %v", address, err)
	}
	return address
}
