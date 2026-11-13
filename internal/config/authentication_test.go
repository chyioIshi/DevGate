package config

import (
	"strings"
	"testing"
)

func TestValidateAuthentication(t *testing.T) {
	tests := []struct {
		name        string
		config      fileConfig
		wantMessage string
	}{
		{
			name: "no authentication configured",
			config: fileConfig{
				Routes: []RouteConfig{{Name: "public"}},
			},
		},
		{
			name:   "valid API key authentication",
			config: validAuthenticationFileConfig(),
		},
		{
			name: "empty credential name",
			config: fileConfig{
				APIKeys: map[string]APIKeyCredentialConfig{
					" ": {ValueEnv: "DEVGATE_API_KEY"},
				},
			},
			wantMessage: "credential name must not be empty",
		},
		{
			name: "credential name with surrounding whitespace",
			config: fileConfig{
				APIKeys: map[string]APIKeyCredentialConfig{
					" internal-service": {ValueEnv: "DEVGATE_API_KEY"},
				},
			},
			wantMessage: "must not have leading or trailing whitespace",
		},
		{
			name: "empty environment variable name",
			config: fileConfig{
				APIKeys: map[string]APIKeyCredentialConfig{
					"internal-service": {},
				},
			},
			wantMessage: `environment variable name for API key "internal-service" must not be empty`,
		},
		{
			name: "environment variable name with surrounding whitespace",
			config: fileConfig{
				APIKeys: map[string]APIKeyCredentialConfig{
					"internal-service": {ValueEnv: "DEVGATE_API_KEY "},
				},
			},
			wantMessage: "must not have leading or trailing whitespace",
		},
		{
			name: "environment variable name without prefix",
			config: fileConfig{
				APIKeys: map[string]APIKeyCredentialConfig{
					"internal-service": {ValueEnv: "API_KEY"},
				},
			},
			wantMessage: "must start with DEVGATE_ prefix",
		},
		{
			name: "authentication without API key policy",
			config: fileConfig{
				Routes: []RouteConfig{
					{Name: "users", Authentication: &AuthenticationConfig{}},
				},
			},
			wantMessage: `api key of route "users" is not provided`,
		},
		{
			name: "empty API key header",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{Credentials: []string{"internal-service"}},
			),
			wantMessage: `api key header of route "users" must not be empty`,
		},
		{
			name: "invalid API key header",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{
					Header:      "Bad Header",
					Credentials: []string{"internal-service"},
				},
			),
			wantMessage: `api key header of route "users" must be valid`,
		},
		{
			name: "no allowed credentials",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{Header: "X-API-Key"},
			),
			wantMessage: `api key credentials of route "users" must not be empty`,
		},
		{
			name: "empty allowed credential",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{
					Header:      "X-API-Key",
					Credentials: []string{" "},
				},
			),
			wantMessage: `credential of route "users" must not be empty`,
		},
		{
			name: "allowed credential with surrounding whitespace",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{
					Header:      "X-API-Key",
					Credentials: []string{"internal-service "},
				},
			),
			wantMessage: "must not have leading or trailing whitespace",
		},
		{
			name: "duplicate allowed credential",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{
					Header: "X-API-Key",
					Credentials: []string{
						"internal-service",
						"internal-service",
					},
				},
			),
			wantMessage: `credential "internal-service" of route "users" is duplicated`,
		},
		{
			name: "unknown allowed credential",
			config: authenticationFileConfigWithPolicy(
				APIKeyAuthenticationConfig{
					Header:      "X-API-Key",
					Credentials: []string{"partner-service"},
				},
			),
			wantMessage: `credential "partner-service" of route "users" must be in api_keys config`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validateAuthentication()
			if tt.wantMessage == "" {
				if err != nil {
					t.Fatalf("validateAuthentication() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validateAuthentication() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf(
					"validateAuthentication() error = %q, want context %q",
					err,
					tt.wantMessage,
				)
			}
		})
	}
}

func TestLoadRejectsInvalidAuthentication(t *testing.T) {
	clearConfigEnv(t)
	configPath := writeConfigFile(t, `
api_keys:
  internal-service:
    value_env: API_KEY
routes:
  - name: users
    path_prefix: /users
    upstream_url: http://users-service:8080
`)
	t.Setenv(envConfigFile, configPath)

	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want authentication validation error")
	}
	assertZeroConfig(t, got)
	if !strings.Contains(err.Error(), "validate route configuration") {
		t.Errorf("Load() error = %q, want route validation context", err)
	}
	if !strings.Contains(err.Error(), "must start with DEVGATE_ prefix") {
		t.Errorf("Load() error = %q, want API key validation context", err)
	}
}

func validAuthenticationFileConfig() fileConfig {
	return authenticationFileConfigWithPolicy(APIKeyAuthenticationConfig{
		Header:      "X-API-Key",
		Credentials: []string{"internal-service"},
	})
}

func authenticationFileConfigWithPolicy(policy APIKeyAuthenticationConfig) fileConfig {
	return fileConfig{
		APIKeys: map[string]APIKeyCredentialConfig{
			"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
		},
		Routes: []RouteConfig{
			{
				Name: "users",
				Authentication: &AuthenticationConfig{
					APIKey: &policy,
				},
			},
		},
	}
}
