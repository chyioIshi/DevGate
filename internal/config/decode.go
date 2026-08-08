package config

import (
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
)

func decodeConfig(r io.Reader) (fileConfig, error) {
	var cfg fileConfig
	decoder := yaml.NewDecoder(r, yaml.DisallowUnknownField())
	if err := decoder.Decode(&cfg); err != nil {
		return fileConfig{}, fmt.Errorf("decode YAML config: %w", err)
	}

	return cfg, nil

}
