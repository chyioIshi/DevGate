package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testRouteConfigYAML = `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
`

const (
	envHTTPAddr                      = "DEVGATE_HTTP_ADDR"
	envReadHeaderTimeout             = "DEVGATE_READ_HEADER_TIMEOUT"
	envIdleTimeout                   = "DEVGATE_IDLE_TIMEOUT"
	envShutdownTimeout               = "DEVGATE_SHUTDOWN_TIMEOUT"
	envUpstreamResponseHeaderTimeout = "DEVGATE_UPSTREAM_RESPONSE_HEADER_TIMEOUT"
	envUpstreamMaxAttempts           = "DEVGATE_UPSTREAM_MAX_ATTEMPTS"
	envUpstreamRetryBaseDelay        = "DEVGATE_UPSTREAM_RETRY_BASE_DELAY"
	envConfigFile                    = "DEVGATE_CONFIG_FILE"
	envLogFormat                     = "DEVGATE_LOG_FORMAT"
	envLogLevel                      = "DEVGATE_LOG_LEVEL"
)

var configEnvKeys = []string{
	envHTTPAddr,
	envReadHeaderTimeout,
	envIdleTimeout,
	envShutdownTimeout,
	envUpstreamResponseHeaderTimeout,
	envUpstreamMaxAttempts,
	envUpstreamRetryBaseDelay,
	envConfigFile,
	envLogFormat,
	envLogLevel,
}

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)
	t.Chdir(t.TempDir())
	if err := os.WriteFile("devgate.yaml", []byte(testRouteConfigYAML), 0o600); err != nil {
		t.Fatalf("write default config file: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:                      ":8080",
		ReadHeaderTimeout:             5 * time.Second,
		IdleTimeout:                   60 * time.Second,
		ShutdownTimeout:               10 * time.Second,
		UpstreamResponseHeaderTimeout: 10 * time.Second,
		UpstreamMaxAttempts:           2,
		UpstreamRetryBaseDelay:        100 * time.Millisecond,
		ConfigFile:                    "devgate.yaml",
		Routes:                        testRouteConfigs(),
		LogFormat:                     "text",
		LogLevel:                      "info",
	}

	assertConfigEqual(t, got, want)
}

func TestLoadOverrides(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv(envHTTPAddr, "127.0.0.1:9090")
	t.Setenv(envReadHeaderTimeout, "2s")
	t.Setenv(envIdleTimeout, "45s")
	t.Setenv(envShutdownTimeout, "7s")
	t.Setenv(envUpstreamResponseHeaderTimeout, "3s")
	t.Setenv(envUpstreamMaxAttempts, "4")
	t.Setenv(envUpstreamRetryBaseDelay, "250ms")
	t.Setenv(envLogFormat, "json")
	t.Setenv(envLogLevel, "debug")
	configPath := writeConfigFile(t, testRouteConfigYAML)
	t.Setenv(envConfigFile, configPath)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:                      "127.0.0.1:9090",
		ReadHeaderTimeout:             2 * time.Second,
		IdleTimeout:                   45 * time.Second,
		ShutdownTimeout:               7 * time.Second,
		UpstreamResponseHeaderTimeout: 3 * time.Second,
		UpstreamMaxAttempts:           4,
		UpstreamRetryBaseDelay:        250 * time.Millisecond,
		ConfigFile:                    configPath,
		Routes:                        testRouteConfigs(),
		LogFormat:                     "json",
		LogLevel:                      "debug",
	}

	assertConfigEqual(t, got, want)
}

func TestLoadRejectsInvalidLogConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		envKey      string
		envValue    string
		wantMessage string
	}{
		{
			name:        "invalid format",
			envKey:      envLogFormat,
			envValue:    "xml",
			wantMessage: `invalid log format: "xml"`,
		},
		{
			name:        "invalid level",
			envKey:      envLogLevel,
			envValue:    "verbose",
			wantMessage: `invalid log level: "verbose"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(test.envKey, test.envValue)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}

			assertZeroConfig(t, got)

			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Load() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestLoadRejectsWhitespaceConfigFilePath(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envConfigFile, " \t ")

	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	assertZeroConfig(t, got)
	if !strings.Contains(err.Error(), "validate config") {
		t.Errorf("Load() error = %q, want validation context", err)
	}
	if !strings.Contains(err.Error(), "config file path must not be empty") {
		t.Errorf("Load() error = %q, want empty config file context", err)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envReadHeaderTimeout, "invalid")
	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want parsing error")
	}

	assertZeroConfig(t, got)

	if !strings.Contains(err.Error(), "parse environment") {
		t.Errorf("Load() error = %q, want parsing context", err)
	}
}

func TestLoadRejectsNonPositiveDurations(t *testing.T) {
	tests := []struct {
		name        string
		envKey      string
		envValue    string
		wantMessage string
	}{
		{
			name:        "zero read header timeout",
			envKey:      envReadHeaderTimeout,
			envValue:    "0s",
			wantMessage: "read header timeout must be positive",
		},
		{
			name:        "negative idle timeout",
			envKey:      envIdleTimeout,
			envValue:    "-1s",
			wantMessage: "idle timeout must be positive",
		},
		{
			name:        "zero shutdown timeout",
			envKey:      envShutdownTimeout,
			envValue:    "0s",
			wantMessage: "shutdown timeout must be positive",
		},
		{
			name:        "zero upstream response header timeout",
			envKey:      envUpstreamResponseHeaderTimeout,
			envValue:    "0s",
			wantMessage: "upstream response header timeout must be positive",
		},
		{
			name:        "negative upstream response header timeout",
			envKey:      envUpstreamResponseHeaderTimeout,
			envValue:    "-1s",
			wantMessage: "upstream response header timeout must be positive",
		},
		{
			name:        "zero upstream retry base delay",
			envKey:      envUpstreamRetryBaseDelay,
			envValue:    "0s",
			wantMessage: "upstream retry base delay must be positive",
		},
		{
			name:        "negative upstream retry base delay",
			envKey:      envUpstreamRetryBaseDelay,
			envValue:    "-1ms",
			wantMessage: "upstream retry base delay must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(test.envKey, test.envValue)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}

			assertZeroConfig(t, got)

			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Load() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestLoadRejectsInvalidUpstreamMaxAttempts(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "zero", value: "0"},
		{name: "negative", value: "-1"},
		{name: "above maximum", value: "6"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envUpstreamMaxAttempts, test.value)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			assertZeroConfig(t, got)
			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(err.Error(), "upstream max attempts must be between 1 and 5") {
				t.Errorf("Load() error = %q, want max attempts context", err)
			}
		})
	}
}

func TestLoadAcceptsUpstreamMaxAttemptsBoundaries(t *testing.T) {
	for _, attempts := range []string{"1", "5"} {
		t.Run(attempts, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envUpstreamMaxAttempts, attempts)
			t.Setenv(envConfigFile, writeConfigFile(t, testRouteConfigYAML))

			if _, err := Load(); err != nil {
				t.Fatalf("Load() error = %v, want valid boundary", err)
			}
		})
	}
}

func TestLoadEmptyHTTPAddressUsesDefault(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envConfigFile, writeConfigFile(t, testRouteConfigYAML))

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.HTTPAddr != ":8080" {
		t.Errorf("Load().HTTPAddr = %q, want %q", got.HTTPAddr, ":8080")
	}
}

func TestConfigValidateRejectsEmptyHTTPAddress(t *testing.T) {
	cfg := Config{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		ConfigFile:        "devgate.yaml",
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want empty address error")
	}
	if !strings.Contains(err.Error(), "http address must not be empty") {
		t.Errorf("validate() error = %q, want empty address context", err)
	}
}

func TestLoadReturnsRouteConfigurationError(t *testing.T) {
	clearConfigEnv(t)
	configPath := filepath.Join(t.TempDir(), "missing.yaml")
	t.Setenv(envConfigFile, configPath)

	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want route configuration error")
	}
	assertZeroConfig(t, got)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load() error = %v, want fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "load route configuration") {
		t.Errorf("Load() error = %q, want route configuration context", err)
	}
	if !strings.Contains(err.Error(), configPath) {
		t.Errorf("Load() error = %q, want path %q", err, configPath)
	}
}

func assertConfigEqual(t *testing.T, got, want Config) {
	t.Helper()

	if got.HTTPAddr != want.HTTPAddr ||
		got.ReadHeaderTimeout != want.ReadHeaderTimeout ||
		got.IdleTimeout != want.IdleTimeout ||
		got.ShutdownTimeout != want.ShutdownTimeout ||
		got.UpstreamResponseHeaderTimeout != want.UpstreamResponseHeaderTimeout ||
		got.UpstreamMaxAttempts != want.UpstreamMaxAttempts ||
		got.UpstreamRetryBaseDelay != want.UpstreamRetryBaseDelay ||
		got.ConfigFile != want.ConfigFile ||
		got.LogFormat != want.LogFormat ||
		got.LogLevel != want.LogLevel {
		t.Errorf("Config scalar fields = %+v, want %+v", got, want)
	}
	if !slices.Equal(got.Routes, want.Routes) {
		t.Errorf("Config.Routes = %+v, want %+v", got.Routes, want.Routes)
	}
}

func assertZeroConfig(t *testing.T, got Config) {
	t.Helper()

	assertConfigEqual(t, got, Config{})
}

func testRouteConfigs() []RouteConfig {
	return []RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: "http://users-service:8080",
		},
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range configEnvKeys {
		value, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}

		t.Cleanup(func() {
			if exists {
				if err := os.Setenv(key, value); err != nil {
					t.Errorf("restore %s: %v", key, err)
				}
				return
			}

			if err := os.Unsetenv(key); err != nil {
				t.Errorf("unset %s during cleanup: %v", key, err)
			}
		})
	}
}
