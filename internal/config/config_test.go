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
)

var configEnvKeys = []string{
	envHTTPAddr,
	envReadHeaderTimeout,
	envIdleTimeout,
	envShutdownTimeout,
}

func TestLoadDefaults(t *testing.T) {
	clearConfigEnv(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:          ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
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

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:          "127.0.0.1:9090",
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       45 * time.Second,
		ShutdownTimeout:   7 * time.Second,
	}

	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envReadHeaderTimeout, "invalid")

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
	}

	err := cfg.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want empty address error")
	}
	if !strings.Contains(err.Error(), "http address must not be empty") {
		t.Errorf("validate() error = %q, want empty address context", err)
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
