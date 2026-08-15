package config

import (
	"fmt"
	"os"
)

func loadConfigFile(path string) (fileConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return fileConfig{}, fmt.Errorf("open config file %q: %w", path, err)
	}
	defer file.Close()
	cfg, err := decodeConfig(file)
	if err != nil {
		return fileConfig{}, fmt.Errorf("load config file %q: %w", path, err)
	}
	return cfg, nil
}
