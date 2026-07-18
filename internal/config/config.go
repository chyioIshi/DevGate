package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	HTTPAddr          string        `env:"DEVGATE_HTTP_ADDR"`
	ReadHeaderTimeout time.Duration `env:"DEVGATE_READ_HEADER_TIMEOUT"`
	IdleTimeout       time.Duration `env:"DEVGATE_IDLE_TIMEOUT"`
	ShutdownTimeout   time.Duration `env:"DEVGATE_SHUTDOWN_TIMEOUT"`
	UpstreamURL       string        `env:"DEVGATE_UPSTREAM_URL"`
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
	if c.UpstreamURL == "" {
		return errors.New("upstream URL must not be empty")
	}
	upstreamURL, err := url.Parse(c.UpstreamURL)
	if err != nil {
		return fmt.Errorf("parse upstream URL: %w", err)
	}
	if upstreamURL.Host == "" {
		return errors.New("upstream URL host must not be empty")
	}
	if upstreamURL.Scheme != "http" && upstreamURL.Scheme != "https" {
		return errors.New("upstream URL scheme must be http or https")
	}
	return nil
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:          ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
	}

	if err := env.Parse(&config); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := config.validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return config, nil
}
