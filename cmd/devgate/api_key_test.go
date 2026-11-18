package main

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
)

func TestNewAPIKeyMiddlewareResolvesConfiguredCredentials(t *testing.T) {
	configuredDigests := map[string]config.APIKeyDigest{
		"client-a": config.APIKeyDigest(sha256.Sum256([]byte("secret-a"))),
		"client-b": config.APIKeyDigest(sha256.Sum256([]byte("secret-b"))),
	}
	policy := router.APIKeyAuthenticationPolicy{
		Header:      "X-API-Key",
		Credentials: []string{"client-a", "client-b"},
	}
	nextCalls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalls++
		w.WriteHeader(http.StatusNoContent)
	})
	handler, err := newAPIKeyMiddleware(next, policy, configuredDigests, nil, nil)
	if err != nil {
		t.Fatalf("newAPIKeyMiddleware() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Api-Key", "secret-b")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if nextCalls != 1 {
		t.Errorf("next calls = %d, want 1", nextCalls)
	}
}

func TestNewAPIKeyMiddlewareRejectsUnknownCredential(t *testing.T) {
	policy := router.APIKeyAuthenticationPolicy{
		Header:      "X-API-Key",
		Credentials: []string{"missing-client"},
	}

	got, err := newAPIKeyMiddleware(
		http.NotFoundHandler(),
		policy,
		map[string]config.APIKeyDigest{},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("newAPIKeyMiddleware() error = nil, want missing credential error")
	}
	if got != nil {
		t.Errorf("newAPIKeyMiddleware() = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), `digest for credential "missing-client" was not found`) {
		t.Errorf("newAPIKeyMiddleware() error = %q, want credential context", err)
	}
}

func TestNewAPIKeyMiddlewareWrapsMiddlewareConfigurationError(t *testing.T) {
	policy := router.APIKeyAuthenticationPolicy{Header: "X-API-Key"}

	got, err := newAPIKeyMiddleware(
		http.NotFoundHandler(),
		policy,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("newAPIKeyMiddleware() error = nil, want middleware configuration error")
	}
	if got != nil {
		t.Errorf("newAPIKeyMiddleware() = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), "create API key middleware: credentials must not be empty") {
		t.Errorf("newAPIKeyMiddleware() error = %q, want construction context", err)
	}
}
