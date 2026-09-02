package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type LogFormat string
type LogLevel string

const (
	LogFormatText LogFormat = "text"
	LogFormatJSON LogFormat = "json"

	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

type Config struct {
	HTTPAddr                      string        `env:"DEVGATE_HTTP_ADDR"`
	ReadHeaderTimeout             time.Duration `env:"DEVGATE_READ_HEADER_TIMEOUT"`
	IdleTimeout                   time.Duration `env:"DEVGATE_IDLE_TIMEOUT"`
	ShutdownTimeout               time.Duration `env:"DEVGATE_SHUTDOWN_TIMEOUT"`
	ConfigFile                    string        `env:"DEVGATE_CONFIG_FILE"`
	Routes                        []RouteConfig
	LogFormat                     LogFormat     `env:"DEVGATE_LOG_FORMAT"`
	LogLevel                      LogLevel      `env:"DEVGATE_LOG_LEVEL"`
	UpstreamResponseHeaderTimeout time.Duration `env:"DEVGATE_UPSTREAM_RESPONSE_HEADER_TIMEOUT"`
	UpstreamMaxAttempts           int           `env:"DEVGATE_UPSTREAM_MAX_ATTEMPTS"`
	UpstreamRetryBaseDelay        time.Duration `env:"DEVGATE_UPSTREAM_RETRY_BASE_DELAY"`
}

func (c Config) validate() error {
	if c.HTTPAddr == "" {
		return errors.New("http address must not be empty")
	}
	if c.ReadHeaderTimeout <= 0 {
		return errors.New("read header timeout must be positive")
	}
	if c.IdleTimeout <= 0 {
		return errors.New("idle timeout must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("shutdown timeout must be positive")
	}
	if strings.TrimSpace(c.ConfigFile) == "" {
		return errors.New("config file path must not be empty")
	}
	switch c.LogFormat {
	case LogFormatJSON, LogFormatText:
	default:
		return fmt.Errorf("invalid log format: %q, must be 'json' or 'text'", c.LogFormat)
	}
	switch c.LogLevel {
	case LogLevelInfo, LogLevelWarn, LogLevelError, LogLevelDebug:
	default:
		return fmt.Errorf("invalid log level: %q, must be 'info', 'warn', 'error', 'debug'", c.LogLevel)
	}
	if c.UpstreamResponseHeaderTimeout <= 0 {
		return errors.New("upstream response header timeout must be positive")
	}
	if c.UpstreamMaxAttempts < 1 || c.UpstreamMaxAttempts > 5 {
		return errors.New("upstream max attempts must be between 1 and 5")
	}
	if c.UpstreamRetryBaseDelay <= 0 {
		return errors.New("upstream retry base delay must be positive")
	}
	return nil
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:                      ":8080",
		ReadHeaderTimeout:             5 * time.Second,
		IdleTimeout:                   60 * time.Second,
		ShutdownTimeout:               10 * time.Second,
		ConfigFile:                    "devgate.yaml",
		LogFormat:                     LogFormatText,
		LogLevel:                      LogLevelInfo,
		UpstreamResponseHeaderTimeout: 10 * time.Second,
		UpstreamMaxAttempts:           2,
		UpstreamRetryBaseDelay:        100 * time.Millisecond,
	}

	if err := env.Parse(&config); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := config.validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	routesConfig, err := loadConfigFile(config.ConfigFile)
	if err != nil {
		return Config{}, fmt.Errorf("load route configuration: %w", err)
	}

	config.Routes = routesConfig.Routes

	return config, nil
}
