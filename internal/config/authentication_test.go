package config

import (
	"crypto/sha256"
	"reflect"
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

func TestResolveAPIKeys(t *testing.T) {
	definitions := map[string]APIKeyCredentialConfig{
		"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
		"partner-service":  {ValueEnv: "DEVGATE_API_KEY_PARTNER_SERVICE"},
	}
	values := map[string]string{
		"DEVGATE_API_KEY_INTERNAL_SERVICE": "internal-secret",
		"DEVGATE_API_KEY_PARTNER_SERVICE":  "partner-secret",
	}
	lookupEnv := func(name string) (string, bool) {
		value, exists := values[name]
		return value, exists
	}

	got, err := resolveAPIKeys(definitions, lookupEnv)
	if err != nil {
		t.Fatalf("resolveAPIKeys() error = %v", err)
	}
	want := map[string]APIKeyDigest{
		"internal-service": APIKeyDigest(sha256.Sum256([]byte("internal-secret"))),
		"partner-service":  APIKeyDigest(sha256.Sum256([]byte("partner-secret"))),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolveAPIKeys() = %x, want %x", got, want)
	}
}

func TestResolveAPIKeysWithoutDefinitionsReturnsNil(t *testing.T) {
	got, err := resolveAPIKeys(nil, func(string) (string, bool) {
		t.Fatal("lookupEnv() called without API key definitions")
		return "", false
	})
	if err != nil {
		t.Fatalf("resolveAPIKeys() error = %v", err)
	}
	if got != nil {
		t.Errorf("resolveAPIKeys() = %v, want nil", got)
	}
}

func TestResolveAPIKeysRejectsInvalidSecrets(t *testing.T) {
	tests := []struct {
		name        string
		definitions map[string]APIKeyCredentialConfig
		values      map[string]string
		wantMessage string
		secret      string
	}{
		{
			name: "missing environment variable",
			definitions: map[string]APIKeyCredentialConfig{
				"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
			},
			wantMessage: `environment value of api key "internal-service" was not defined`,
		},
		{
			name: "empty secret",
			definitions: map[string]APIKeyCredentialConfig{
				"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
			},
			values: map[string]string{
				"DEVGATE_API_KEY_INTERNAL_SERVICE": "",
			},
			wantMessage: `environment value of api key "internal-service" must not be empty`,
		},
		{
			name: "secret with surrounding whitespace",
			definitions: map[string]APIKeyCredentialConfig{
				"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
			},
			values: map[string]string{
				"DEVGATE_API_KEY_INTERNAL_SERVICE": " secret-value ",
			},
			wantMessage: "must not have leading or trailing whitespace",
			secret:      " secret-value ",
		},
		{
			name: "invalid header value",
			definitions: map[string]APIKeyCredentialConfig{
				"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
			},
			values: map[string]string{
				"DEVGATE_API_KEY_INTERNAL_SERVICE": "secret\r\ninjected",
			},
			wantMessage: "header value of api key",
			secret:      "secret\r\ninjected",
		},
		{
			name: "duplicate secret",
			definitions: map[string]APIKeyCredentialConfig{
				"internal-service": {ValueEnv: "DEVGATE_API_KEY_INTERNAL_SERVICE"},
				"partner-service":  {ValueEnv: "DEVGATE_API_KEY_PARTNER_SERVICE"},
			},
			values: map[string]string{
				"DEVGATE_API_KEY_INTERNAL_SERVICE": "shared-secret",
				"DEVGATE_API_KEY_PARTNER_SERVICE":  "shared-secret",
			},
			wantMessage: "resolve to the same secret",
			secret:      "shared-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookupEnv := func(name string) (string, bool) {
				value, exists := tt.values[name]
				return value, exists
			}

			got, err := resolveAPIKeys(tt.definitions, lookupEnv)
			if err == nil {
				t.Fatal("resolveAPIKeys() error = nil, want validation error")
			}
			if got != nil {
				t.Errorf("resolveAPIKeys() = %v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf(
					"resolveAPIKeys() error = %q, want context %q",
					err,
					tt.wantMessage,
				)
			}
			if tt.secret != "" && strings.Contains(err.Error(), tt.secret) {
				t.Errorf("resolveAPIKeys() error contains API key secret")
			}
		})
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
