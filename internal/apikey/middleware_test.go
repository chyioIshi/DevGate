package apikey

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewValidatesConfiguration(t *testing.T) {
	validDigest := digestForTest("secret")
	tests := []struct {
		name        string
		next        http.Handler
		header      string
		credentials []Credential
		wantMessage string
	}{
		{
			name:        "nil next handler",
			header:      "X-API-Key",
			credentials: []Credential{{Name: "client", Digest: validDigest}},
			wantMessage: "next handler must not be nil",
		},
		{
			name:        "empty header",
			next:        http.NotFoundHandler(),
			credentials: []Credential{{Name: "client", Digest: validDigest}},
			wantMessage: "header must not be empty",
		},
		{
			name:        "header with surrounding whitespace",
			next:        http.NotFoundHandler(),
			header:      " X-API-Key",
			credentials: []Credential{{Name: "client", Digest: validDigest}},
			wantMessage: "header must not have leading or trailing whitespace",
		},
		{
			name:        "invalid header",
			next:        http.NotFoundHandler(),
			header:      "Bad Header",
			credentials: []Credential{{Name: "client", Digest: validDigest}},
			wantMessage: "must be valid",
		},
		{
			name:        "no credentials",
			next:        http.NotFoundHandler(),
			header:      "X-API-Key",
			wantMessage: "credentials must not be empty",
		},
		{
			name:        "empty credential name",
			next:        http.NotFoundHandler(),
			header:      "X-API-Key",
			credentials: []Credential{{Digest: validDigest}},
			wantMessage: "credential name must not be empty",
		},
		{
			name:   "credential name with surrounding whitespace",
			next:   http.NotFoundHandler(),
			header: "X-API-Key",
			credentials: []Credential{
				{Name: " client", Digest: validDigest},
			},
			wantMessage: "must not have leading or trailing whitespace",
		},
		{
			name:   "duplicate credential name",
			next:   http.NotFoundHandler(),
			header: "X-API-Key",
			credentials: []Credential{
				{Name: "client", Digest: validDigest},
				{Name: "client", Digest: digestForTest("other-secret")},
			},
			wantMessage: "credential name \"client\" is duplicated",
		},
		{
			name:   "zero digest",
			next:   http.NotFoundHandler(),
			header: "X-API-Key",
			credentials: []Credential{
				{Name: "client"},
			},
			wantMessage: "digest must not be zero",
		},
		{
			name:   "duplicate digest",
			next:   http.NotFoundHandler(),
			header: "X-API-Key",
			credentials: []Credential{
				{Name: "client-a", Digest: validDigest},
				{Name: "client-b", Digest: validDigest},
			},
			wantMessage: "have the same digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.next, tt.header, tt.credentials, nil)
			if err == nil {
				t.Fatal("New() error = nil, want configuration error")
			}
			if got != nil {
				t.Errorf("New() = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, tt.wantMessage)
			}
		})
	}
}

func TestMiddlewareAuthenticatesAnyConfiguredCredential(t *testing.T) {
	credentials := []Credential{
		{Name: "client-a", Digest: digestForTest("secret-a")},
		{Name: "client-b", Digest: digestForTest("secret-b")},
	}

	for _, apiKey := range []string{"secret-a", "secret-b"} {
		t.Run(apiKey, func(t *testing.T) {
			nextCalls := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls++
				if got := r.Header.Get("X-Api-Key"); got != "" {
					t.Errorf("downstream API key header = %q, want removed", got)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			handler, err := New(next, "X-API-Key", credentials, nil)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-Api-Key", apiKey)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNoContent {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
			}
			if nextCalls != 1 {
				t.Errorf("next calls = %d, want 1", nextCalls)
			}
		})
	}
}

func TestMiddlewareRejectsMissingMultipleAndInvalidKeys(t *testing.T) {
	tests := []struct {
		name   string
		values []string
	}{
		{name: "missing"},
		{name: "multiple", values: []string{"secret", "other"}},
		{name: "invalid", values: []string{"wrong-secret"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalls := 0
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				nextCalls++
			})
			handler, err := New(
				next,
				"X-API-Key",
				[]Credential{{Name: "client", Digest: digestForTest("secret")}},
				nil,
			)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, value := range tt.values {
				request.Header.Add("X-Api-Key", value)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			if got := recorder.Header().Get("WWW-Authenticate"); got != "ApiKey" {
				t.Errorf("WWW-Authenticate = %q, want %q", got, "ApiKey")
			}
			if nextCalls != 0 {
				t.Errorf("next calls = %d, want 0", nextCalls)
			}
		})
	}
}

func TestMiddlewareUsesCustomErrorResponder(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler called for unauthorized request")
	})
	responderCalls := 0
	errorResponder := func(w http.ResponseWriter, _ *http.Request, statusCode int) {
		responderCalls++
		if statusCode != http.StatusUnauthorized {
			t.Errorf("status code = %d, want %d", statusCode, http.StatusUnauthorized)
		}
		if got := w.Header().Get("WWW-Authenticate"); got != "ApiKey" {
			t.Errorf("WWW-Authenticate = %q, want %q", got, "ApiKey")
		}
		w.WriteHeader(statusCode)
	}
	handler, err := New(
		next,
		"X-API-Key",
		[]Credential{{Name: "client", Digest: digestForTest("secret")}},
		errorResponder,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil),
	)

	if responderCalls != 1 {
		t.Errorf("error responder calls = %d, want 1", responderCalls)
	}
}

func TestMiddlewareOwnsCredentialSnapshot(t *testing.T) {
	credentials := []Credential{
		{Name: "client", Digest: digestForTest("original-secret")},
	}
	handler, err := New(http.NotFoundHandler(), "X-API-Key", credentials, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	credentials[0].Digest = digestForTest("mutated-secret")

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Api-Key", "original-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want downstream status %d", recorder.Code, http.StatusNotFound)
	}
}

func digestForTest(secret string) Digest {
	return Digest(sha256.Sum256([]byte(secret)))
}
