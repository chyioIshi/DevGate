package router

import (
	"strings"
	"testing"
)

func TestNewValidatesAuthenticationPolicy(t *testing.T) {
	tests := []struct {
		name           string
		authentication *AuthenticationPolicy
		wantMessage    string
	}{
		{
			name: "valid API key policy",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Header:      "X-API-Key",
					Credentials: []string{"internal-service", "partner-service"},
				},
			},
		},
		{
			name:           "missing API key policy",
			authentication: &AuthenticationPolicy{},
			wantMessage:    "API key was not provided",
		},
		{
			name: "empty header",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Credentials: []string{"internal-service"},
				},
			},
			wantMessage: "header name of API key must not be empty",
		},
		{
			name: "header with surrounding whitespace",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Header:      " X-API-Key",
					Credentials: []string{"internal-service"},
				},
			},
			wantMessage: "header name of API key must not have leading or trailing whitespace",
		},
		{
			name: "invalid header",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Header:      "Bad Header",
					Credentials: []string{"internal-service"},
				},
			},
			wantMessage: "header name of API key must be valid",
		},
		{
			name: "no credentials",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{Header: "X-API-Key"},
			},
			wantMessage: "credentials of API key must not be empty",
		},
		{
			name: "empty credential",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Header:      "X-API-Key",
					Credentials: []string{" "},
				},
			},
			wantMessage: "credential of API key must not be empty",
		},
		{
			name: "credential with surrounding whitespace",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Header:      "X-API-Key",
					Credentials: []string{"internal-service "},
				},
			},
			wantMessage: `credential "internal-service " of API key must not have leading or trailing whitespace`,
		},
		{
			name: "duplicate credential",
			authentication: &AuthenticationPolicy{
				APIKey: &APIKeyAuthenticationPolicy{
					Header:      "X-API-Key",
					Credentials: []string{"internal-service", "internal-service"},
				},
			},
			wantMessage: `credential "internal-service" is duplicated`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := Route{
				Name:           "users",
				PathPrefix:     "/users",
				DirectResponse: &DirectResponse{StatusCode: 200},
				Authentication: tt.authentication,
			}

			got, err := New([]Route{route})
			if tt.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() = nil, want router")
				}
				return
			}
			if err == nil {
				t.Fatal("New() error = nil, want authentication validation error")
			}
			if got != nil {
				t.Errorf("New() = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), "authentication policy:") {
				t.Errorf("New() error = %q, want authentication policy context", err)
			}
			if tt.authentication.APIKey != nil &&
				!strings.Contains(err.Error(), "API key policy:") {
				t.Errorf("New() error = %q, want API key policy context", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, tt.wantMessage)
			}
		})
	}
}
