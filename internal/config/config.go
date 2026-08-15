package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	HTTPAddr          string        `env:"DEVGATE_HTTP_ADDR"`
	ReadHeaderTimeout time.Duration `env:"DEVGATE_READ_HEADER_TIMEOUT"`
	IdleTimeout       time.Duration `env:"DEVGATE_IDLE_TIMEOUT"`
	ShutdownTimeout   time.Duration `env:"DEVGATE_SHUTDOWN_TIMEOUT"`
	ConfigFile        string        `env:"DEVGATE_CONFIG_FILE"`
	Routes            []RouteConfig
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
	return nil
}

func Load() (Config, error) {
	config := Config{
		HTTPAddr:          ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   10 * time.Second,
		ConfigFile:        "devgate.yaml",
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
