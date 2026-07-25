package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

const (
	envHTTPAddr          = "DEVGATE_HTTP_ADDR"
	envReadHeaderTimeout = "DEVGATE_READ_HEADER_TIMEOUT"
	envIdleTimeout       = "DEVGATE_IDLE_TIMEOUT"
	envShutdownTimeout   = "DEVGATE_SHUTDOWN_TIMEOUT"
	envUpstreamURL       = "DEVGATE_UPSTREAM_URL"
)

var configEnvKeys = []string{
	envHTTPAddr,
	envReadHeaderTimeout,
	envIdleTimeout,
	envShutdownTimeout,
	envUpstreamURL,
}

func TestLoadDefaultsWithRequiredUpstream(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envUpstreamURL, "http://localhost:8081")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:          ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		UpstreamURL:       "http://localhost:8081",
	}

	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv(envHTTPAddr, "127.0.0.1:9090")
	t.Setenv(envReadHeaderTimeout, "2s")
	t.Setenv(envIdleTimeout, "45s")
	t.Setenv(envShutdownTimeout, "7s")
	t.Setenv(envUpstreamURL, "https://localhost:8081")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:          "127.0.0.1:9090",
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       45 * time.Second,
		ShutdownTimeout:   7 * time.Second,
		UpstreamURL:       "https://localhost:8081",
	}

	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envReadHeaderTimeout, "invalid")
	t.Setenv(envUpstreamURL, "http://localhost:8081")
	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want parsing error")
	}

	if got != (Config{}) {
		t.Errorf("Load() = %+v, want zero Config", got)
	}

	if !strings.Contains(err.Error(), "parse environment") {
		t.Errorf("Load() error = %q, want parsing context", err)
	}
}

func TestLoadRejectsNonPositiveTimeouts(t *testing.T) {
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(test.envKey, test.envValue)
			t.Setenv(envUpstreamURL, "http://localhost:8081")

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}

			if got != (Config{}) {
				t.Errorf("Load() = %+v, want zero Config", got)
			}

			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Load() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestLoadEmptyHTTPAddressUsesDefault(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envUpstreamURL, "http://localhost:8081")

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
		UpstreamURL:       "http://localhost:8081",
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want empty address error")
	}
	if !strings.Contains(err.Error(), "http address must not be empty") {
		t.Errorf("validate() error = %q, want empty address context", err)
	}
}

func TestLoadRejectsInvalidUpstreamURL(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		wantMessage string
	}{
		{
			"empty upstream URL",
			"",
			"upstream URL must not be empty",
		},
		{
			"malformed upstream URL scheme",
			"://broken.com",
			"parse upstream URL",
		},
		{
			"empty upstream URL host",
			"http:///api",
			"upstream URL host must not be empty",
		},
		{
			"unsupported upstream URL scheme",
			"ftp://ftp.com",
			"upstream URL scheme must be http or https",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envUpstreamURL, test.envValue)

			got, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want validation error")
			}

			if got != (Config{}) {
				t.Errorf("Load() = %+v, want zero Config", got)
			}

			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}

			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Load() error = %q, want %q", err, test.wantMessage)
			}

		})
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
