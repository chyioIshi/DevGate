package config

import (
	"errors"
	"io/fs"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
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
	envHTTPAddr                        = "DEVGATE_HTTP_ADDR"
	envReadHeaderTimeout               = "DEVGATE_READ_HEADER_TIMEOUT"
	envMaxHeaderBytes                  = "DEVGATE_MAX_HEADER_BYTES"
	envIdleTimeout                     = "DEVGATE_IDLE_TIMEOUT"
	envShutdownTimeout                 = "DEVGATE_SHUTDOWN_TIMEOUT"
	envUpstreamMaxIdleConns            = "DEVGATE_UPSTREAM_MAX_IDLE_CONNS"
	envUpstreamMaxIdleConnsPerHost     = "DEVGATE_UPSTREAM_MAX_IDLE_CONNS_PER_HOST"
	envUpstreamMaxConnsPerHost         = "DEVGATE_UPSTREAM_MAX_CONNS_PER_HOST"
	envUpstreamIdleConnTimeout         = "DEVGATE_UPSTREAM_IDLE_CONN_TIMEOUT"
	envUpstreamResponseHeaderTimeout   = "DEVGATE_UPSTREAM_RESPONSE_HEADER_TIMEOUT"
	envUpstreamMaxAttempts             = "DEVGATE_UPSTREAM_MAX_ATTEMPTS"
	envUpstreamRetryBaseDelay          = "DEVGATE_UPSTREAM_RETRY_BASE_DELAY"
	envUpstreamCircuitFailureThreshold = "DEVGATE_UPSTREAM_CIRCUIT_FAILURE_THRESHOLD"
	envUpstreamCircuitOpenTimeout      = "DEVGATE_UPSTREAM_CIRCUIT_OPEN_TIMEOUT"
	envTrustedProxyCIDRs               = "DEVGATE_TRUSTED_PROXY_CIDRS"
	envConfigFile                      = "DEVGATE_CONFIG_FILE"
	envLogFormat                       = "DEVGATE_LOG_FORMAT"
	envLogLevel                        = "DEVGATE_LOG_LEVEL"
	envTLSCertFile                     = "DEVGATE_TLS_CERT_FILE"
	envTLSKeyFile                      = "DEVGATE_TLS_KEY_FILE"
	envRedisAddr                       = "DEVGATE_REDIS_ADDR"
	envRedisUsername                   = "DEVGATE_REDIS_USERNAME"
	envRedisPassword                   = "DEVGATE_REDIS_PASSWORD"
	envRedisDB                         = "DEVGATE_REDIS_DB"
	envRedisDialTimeout                = "DEVGATE_REDIS_DIAL_TIMEOUT"
	envRedisReadTimeout                = "DEVGATE_REDIS_READ_TIMEOUT"
	envRedisWriteTimeout               = "DEVGATE_REDIS_WRITE_TIMEOUT"
	envRedisKeyPrefix                  = "DEVGATE_REDIS_KEY_PREFIX"
)

var configEnvKeys = []string{
	envHTTPAddr,
	envReadHeaderTimeout,
	envMaxHeaderBytes,
	envIdleTimeout,
	envShutdownTimeout,
	envUpstreamMaxIdleConns,
	envUpstreamMaxIdleConnsPerHost,
	envUpstreamMaxConnsPerHost,
	envUpstreamIdleConnTimeout,
	envUpstreamResponseHeaderTimeout,
	envUpstreamMaxAttempts,
	envUpstreamRetryBaseDelay,
	envUpstreamCircuitFailureThreshold,
	envUpstreamCircuitOpenTimeout,
	envTrustedProxyCIDRs,
	envConfigFile,
	envLogFormat,
	envLogLevel,
	envTLSCertFile,
	envTLSKeyFile,
	envRedisAddr,
	envRedisUsername,
	envRedisPassword,
	envRedisDB,
	envRedisDialTimeout,
	envRedisReadTimeout,
	envRedisWriteTimeout,
	envRedisKeyPrefix,
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
		HTTPAddr:                        ":8080",
		ReadHeaderTimeout:               5 * time.Second,
		MaxHeaderBytes:                  64 * 1024,
		IdleTimeout:                     60 * time.Second,
		ShutdownTimeout:                 10 * time.Second,
		UpstreamMaxIdleConns:            100,
		UpstreamMaxIdleConnsPerHost:     20,
		UpstreamMaxConnsPerHost:         100,
		UpstreamIdleConnTimeout:         90 * time.Second,
		UpstreamResponseHeaderTimeout:   10 * time.Second,
		UpstreamMaxAttempts:             2,
		UpstreamRetryBaseDelay:          100 * time.Millisecond,
		UpstreamCircuitFailureThreshold: 5,
		UpstreamCircuitOpenTimeout:      30 * time.Second,
		ConfigFile:                      "devgate.yaml",
		Routes:                          testRouteConfigs(),
		LogFormat:                       "text",
		LogLevel:                        "info",
		Redis: RedisConfig{
			DialTimeout:  time.Second,
			ReadTimeout:  200 * time.Millisecond,
			WriteTimeout: 200 * time.Millisecond,
			KeyPrefix:    "devgate",
		},
	}

	assertConfigEqual(t, got, want)
}

func TestLoadOverrides(t *testing.T) {
	clearConfigEnv(t)

	t.Setenv(envHTTPAddr, "127.0.0.1:9090")
	t.Setenv(envReadHeaderTimeout, "2s")
	t.Setenv(envMaxHeaderBytes, "32768")
	t.Setenv(envIdleTimeout, "45s")
	t.Setenv(envShutdownTimeout, "7s")
	t.Setenv(envUpstreamMaxIdleConns, "200")
	t.Setenv(envUpstreamMaxIdleConnsPerHost, "40")
	t.Setenv(envUpstreamMaxConnsPerHost, "150")
	t.Setenv(envUpstreamIdleConnTimeout, "30s")
	t.Setenv(envUpstreamResponseHeaderTimeout, "3s")
	t.Setenv(envUpstreamMaxAttempts, "4")
	t.Setenv(envUpstreamRetryBaseDelay, "250ms")
	t.Setenv(envUpstreamCircuitFailureThreshold, "7")
	t.Setenv(envUpstreamCircuitOpenTimeout, "45s")
	t.Setenv(envTrustedProxyCIDRs, "10.0.0.10/32,2001:db8::10/128")
	t.Setenv(envLogFormat, "json")
	t.Setenv(envLogLevel, "debug")
	t.Setenv(envTLSCertFile, "/run/secrets/devgate/tls.crt")
	t.Setenv(envTLSKeyFile, "/run/secrets/devgate/tls.key")
	t.Setenv(envRedisAddr, "redis.internal:6379")
	t.Setenv(envRedisUsername, "devgate")
	t.Setenv(envRedisPassword, "test-password")
	t.Setenv(envRedisDB, "2")
	t.Setenv(envRedisDialTimeout, "750ms")
	t.Setenv(envRedisReadTimeout, "150ms")
	t.Setenv(envRedisWriteTimeout, "175ms")
	t.Setenv(envRedisKeyPrefix, "prod:payments")
	configPath := writeConfigFile(t, testRouteConfigYAML)
	t.Setenv(envConfigFile, configPath)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:                        "127.0.0.1:9090",
		ReadHeaderTimeout:               2 * time.Second,
		MaxHeaderBytes:                  32 * 1024,
		IdleTimeout:                     45 * time.Second,
		ShutdownTimeout:                 7 * time.Second,
		UpstreamMaxIdleConns:            200,
		UpstreamMaxIdleConnsPerHost:     40,
		UpstreamMaxConnsPerHost:         150,
		UpstreamIdleConnTimeout:         30 * time.Second,
		UpstreamResponseHeaderTimeout:   3 * time.Second,
		UpstreamMaxAttempts:             4,
		UpstreamRetryBaseDelay:          250 * time.Millisecond,
		UpstreamCircuitFailureThreshold: 7,
		UpstreamCircuitOpenTimeout:      45 * time.Second,
		TrustedProxyCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.10/32"),
			netip.MustParsePrefix("2001:db8::10/128"),
		},
		ConfigFile:  configPath,
		Routes:      testRouteConfigs(),
		LogFormat:   "json",
		LogLevel:    "debug",
		TLSCertFile: "/run/secrets/devgate/tls.crt",
		TLSKeyFile:  "/run/secrets/devgate/tls.key",
		Redis: RedisConfig{
			Addr:         "redis.internal:6379",
			Username:     "devgate",
			Password:     "test-password",
			DB:           2,
			DialTimeout:  750 * time.Millisecond,
			ReadTimeout:  150 * time.Millisecond,
			WriteTimeout: 175 * time.Millisecond,
			KeyPrefix:    "prod:payments",
		},
	}

	assertConfigEqual(t, got, want)
}

func TestLoadGlobalErrorResponses(t *testing.T) {
	clearConfigEnv(t)
	configPath := writeConfigFile(t, `
error_responses:
  404:
    body: '{"error":"not found"}'
    headers:
      Content-Type: application/json
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
`)
	t.Setenv(envConfigFile, configPath)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := map[int]ErrorResponseConfig{
		http.StatusNotFound: {
			Body:    `{"error":"not found"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
	}
	if !reflect.DeepEqual(got.ErrorResponses, want) {
		t.Errorf("Config.ErrorResponses = %+v, want %+v", got.ErrorResponses, want)
	}
}

func TestLoadRejectsInvalidTrustedProxyCIDRs(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		wantContext string
		wantMessage string
	}{
		{
			name:        "malformed CIDR",
			value:       "not-a-cidr",
			wantContext: "parse environment",
			wantMessage: "TrustedProxyCIDRs",
		},
		{
			name:        "non-canonical network",
			value:       "10.0.0.10/24",
			wantContext: "validate config",
			wantMessage: "must be masked as 10.0.0.0/24",
		},
		{
			name:        "trust all IPv4 addresses",
			value:       "0.0.0.0/0",
			wantContext: "validate config",
			wantMessage: "must not trust all addresses",
		},
		{
			name:        "trust all IPv6 addresses",
			value:       "::/0",
			wantContext: "validate config",
			wantMessage: "must not trust all addresses",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envTrustedProxyCIDRs, test.value)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want trusted proxy CIDR error")
			}
			assertZeroConfig(t, got)
			if !strings.Contains(err.Error(), test.wantContext) {
				t.Errorf("Load() error = %q, want context %q", err, test.wantContext)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("Load() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestConfigValidateRejectsInvalidTrustedProxyPrefix(t *testing.T) {
	cfg := validConfigForTest()
	cfg.TrustedProxyCIDRs = []netip.Prefix{{}}

	err := cfg.validate()
	if err == nil {
		t.Fatal("validate() error = nil, want invalid trusted proxy CIDR error")
	}
	if !strings.Contains(err.Error(), "trusted proxy CIDR") ||
		!strings.Contains(err.Error(), "is invalid") {
		t.Errorf("validate() error = %q, want invalid trusted proxy CIDR context", err)
	}
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

func TestLoadRejectsIncompleteTLSConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "certificate without key",
			key:   envTLSCertFile,
			value: "/run/secrets/devgate/tls.crt",
		},
		{
			name:  "key without certificate",
			key:   envTLSKeyFile,
			value: "/run/secrets/devgate/tls.key",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(test.key, test.value)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want TLS validation error")
			}

			assertZeroConfig(t, got)
			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(
				err.Error(),
				"TLS certificate and key files must be configured together",
			) {
				t.Errorf("Load() error = %q, want incomplete TLS configuration context", err)
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
			name:        "zero upstream idle connection timeout",
			envKey:      envUpstreamIdleConnTimeout,
			envValue:    "0s",
			wantMessage: "upstream idle connection timeout must be positive",
		},
		{
			name:        "negative upstream idle connection timeout",
			envKey:      envUpstreamIdleConnTimeout,
			envValue:    "-1s",
			wantMessage: "upstream idle connection timeout must be positive",
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
		{
			name:        "zero upstream circuit open timeout",
			envKey:      envUpstreamCircuitOpenTimeout,
			envValue:    "0s",
			wantMessage: "upstream circuit open timeout must be positive",
		},
		{
			name:        "negative upstream circuit open timeout",
			envKey:      envUpstreamCircuitOpenTimeout,
			envValue:    "-1s",
			wantMessage: "upstream circuit open timeout must be positive",
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

func TestLoadRejectsInvalidRedisConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		envKey      string
		envValue    string
		wantMessage string
	}{
		{
			name:        "negative database",
			envKey:      envRedisDB,
			envValue:    "-1",
			wantMessage: "redis db must be non-negative",
		},
		{
			name:        "zero dial timeout",
			envKey:      envRedisDialTimeout,
			envValue:    "0s",
			wantMessage: "redis dial timeout must be positive",
		},
		{
			name:        "negative read timeout",
			envKey:      envRedisReadTimeout,
			envValue:    "-1ms",
			wantMessage: "redis read timeout must be positive",
		},
		{
			name:        "zero write timeout",
			envKey:      envRedisWriteTimeout,
			envValue:    "0s",
			wantMessage: "redis write timeout must be positive",
		},
		{
			name:        "whitespace key prefix",
			envKey:      envRedisKeyPrefix,
			envValue:    " \t ",
			wantMessage: "redis key prefix must not be empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envRedisAddr, "redis.internal:6379")
			t.Setenv(test.envKey, test.envValue)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want Redis validation error")
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

func TestLoadRejectsInvalidRedisDuration(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envRedisDialTimeout, "slow")

	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want Redis duration parsing error")
	}
	assertZeroConfig(t, got)
	if !strings.Contains(err.Error(), "parse environment") {
		t.Errorf("Load() error = %q, want parsing context", err)
	}
}

func TestLoadRejectsNonPositiveUpstreamConnectionLimits(t *testing.T) {
	tests := []struct {
		name        string
		envKey      string
		wantMessage string
	}{
		{
			name:        "max idle connections",
			envKey:      envUpstreamMaxIdleConns,
			wantMessage: "upstream max idle connections must be positive",
		},
		{
			name:        "max idle connections per host",
			envKey:      envUpstreamMaxIdleConnsPerHost,
			wantMessage: "upstream max idle connections per host must be positive",
		},
		{
			name:        "max connections per host",
			envKey:      envUpstreamMaxConnsPerHost,
			wantMessage: "upstream max connections per host must be positive",
		},
	}

	for _, test := range tests {
		for _, value := range []string{"0", "-1"} {
			t.Run(test.name+"/"+value, func(t *testing.T) {
				clearConfigEnv(t)
				t.Setenv(test.envKey, value)

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
}

func TestLoadRejectsNonPositiveMaxHeaderBytes(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envMaxHeaderBytes, value)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			assertZeroConfig(t, got)
			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(err.Error(), "max request header bytes must be positive") {
				t.Errorf("Load() error = %q, want max header bytes context", err)
			}
		})
	}
}

func TestLoadRejectsInvalidMaxHeaderBytes(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv(envMaxHeaderBytes, "many")

	got, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want parsing error")
	}
	assertZeroConfig(t, got)
	if !strings.Contains(err.Error(), "parse environment") {
		t.Errorf("Load() error = %q, want parsing context", err)
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

func TestLoadRejectsInvalidUpstreamCircuitFailureThreshold(t *testing.T) {
	for _, threshold := range []string{"0", "-1"} {
		t.Run(threshold, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(envUpstreamCircuitFailureThreshold, threshold)

			got, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			assertZeroConfig(t, got)
			if !strings.Contains(err.Error(), "validate config") {
				t.Errorf("Load() error = %q, want validation context", err)
			}
			if !strings.Contains(err.Error(), "upstream circuit failure threshold must be positive") {
				t.Errorf("Load() error = %q, want circuit failure threshold context", err)
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
		got.MaxHeaderBytes != want.MaxHeaderBytes ||
		got.IdleTimeout != want.IdleTimeout ||
		got.ShutdownTimeout != want.ShutdownTimeout ||
		got.UpstreamMaxIdleConns != want.UpstreamMaxIdleConns ||
		got.UpstreamMaxIdleConnsPerHost != want.UpstreamMaxIdleConnsPerHost ||
		got.UpstreamMaxConnsPerHost != want.UpstreamMaxConnsPerHost ||
		got.UpstreamIdleConnTimeout != want.UpstreamIdleConnTimeout ||
		got.UpstreamResponseHeaderTimeout != want.UpstreamResponseHeaderTimeout ||
		got.UpstreamMaxAttempts != want.UpstreamMaxAttempts ||
		got.UpstreamRetryBaseDelay != want.UpstreamRetryBaseDelay ||
		got.UpstreamCircuitFailureThreshold != want.UpstreamCircuitFailureThreshold ||
		got.UpstreamCircuitOpenTimeout != want.UpstreamCircuitOpenTimeout ||
		got.ConfigFile != want.ConfigFile ||
		got.LogFormat != want.LogFormat ||
		got.LogLevel != want.LogLevel ||
		got.TLSCertFile != want.TLSCertFile ||
		got.TLSKeyFile != want.TLSKeyFile {
		gotForLog := got
		wantForLog := want
		gotForLog.Redis.Password = "[redacted]"
		wantForLog.Redis.Password = "[redacted]"
		t.Errorf("Config scalar fields = %+v, want %+v", gotForLog, wantForLog)
	}
	if got.Redis.Addr != want.Redis.Addr ||
		got.Redis.Username != want.Redis.Username ||
		got.Redis.DB != want.Redis.DB ||
		got.Redis.DialTimeout != want.Redis.DialTimeout ||
		got.Redis.ReadTimeout != want.Redis.ReadTimeout ||
		got.Redis.WriteTimeout != want.Redis.WriteTimeout ||
		got.Redis.KeyPrefix != want.Redis.KeyPrefix {
		t.Errorf(
			"Config.Redis non-secret fields differ: "+
				"got addr=%q username=%q db=%d dial=%s read=%s write=%s prefix=%q; "+
				"want addr=%q username=%q db=%d dial=%s read=%s write=%s prefix=%q",
			got.Redis.Addr,
			got.Redis.Username,
			got.Redis.DB,
			got.Redis.DialTimeout,
			got.Redis.ReadTimeout,
			got.Redis.WriteTimeout,
			got.Redis.KeyPrefix,
			want.Redis.Addr,
			want.Redis.Username,
			want.Redis.DB,
			want.Redis.DialTimeout,
			want.Redis.ReadTimeout,
			want.Redis.WriteTimeout,
			want.Redis.KeyPrefix,
		)
	}
	if got.Redis.Password != want.Redis.Password {
		t.Error("Config.Redis.Password differs from expected value")
	}
	if !reflect.DeepEqual(got.Routes, want.Routes) {
		t.Errorf("Config.Routes = %+v, want %+v", got.Routes, want.Routes)
	}
	if !reflect.DeepEqual(got.ErrorResponses, want.ErrorResponses) {
		t.Errorf(
			"Config.ErrorResponses = %+v, want %+v",
			got.ErrorResponses,
			want.ErrorResponses,
		)
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want.TrustedProxyCIDRs) {
		t.Errorf(
			"Config.TrustedProxyCIDRs = %v, want %v",
			got.TrustedProxyCIDRs,
			want.TrustedProxyCIDRs,
		)
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

func validConfigForTest() Config {
	return Config{
		HTTPAddr:                        ":8080",
		ReadHeaderTimeout:               5 * time.Second,
		MaxHeaderBytes:                  64 * 1024,
		IdleTimeout:                     60 * time.Second,
		ShutdownTimeout:                 10 * time.Second,
		ConfigFile:                      "devgate.yaml",
		LogFormat:                       LogFormatText,
		LogLevel:                        LogLevelInfo,
		UpstreamMaxIdleConns:            100,
		UpstreamMaxIdleConnsPerHost:     20,
		UpstreamMaxConnsPerHost:         100,
		UpstreamIdleConnTimeout:         90 * time.Second,
		UpstreamResponseHeaderTimeout:   10 * time.Second,
		UpstreamMaxAttempts:             2,
		UpstreamRetryBaseDelay:          100 * time.Millisecond,
		UpstreamCircuitFailureThreshold: 5,
		UpstreamCircuitOpenTimeout:      30 * time.Second,
		Redis: RedisConfig{
			KeyPrefix: "devgate",
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
