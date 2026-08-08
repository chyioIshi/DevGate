package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadConfigFile(t *testing.T) {
	path := writeConfigFile(t, `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
`)
	want := []RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: "http://users-service:8080",
		},
	}

	got, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("loadConfigFile() error = %v", err)
	}

	if !slices.Equal(got.Routes, want) {
		t.Errorf("loadConfigFile().Routes = %+v, want %+v", got.Routes, want)
	}
}

func TestLoadConfigFileReturnsOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	got, err := loadConfigFile(path)
	if err == nil {
		t.Fatal("loadConfigFile() error = nil, want open error")
	}
	if got.Routes != nil {
		t.Errorf("loadConfigFile().Routes = %+v, want nil", got.Routes)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("loadConfigFile() error = %v, want fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), "open config file") {
		t.Errorf("loadConfigFile() error = %q, want open context", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("loadConfigFile() error = %q, want path %q", err, path)
	}
}

func TestLoadConfigFileReturnsDecodeError(t *testing.T) {
	path := writeConfigFile(t, `
routes:
  - name: users
    protocol: [http
`)

	got, err := loadConfigFile(path)
	if err == nil {
		t.Fatal("loadConfigFile() error = nil, want decode error")
	}
	if got.Routes != nil {
		t.Errorf("loadConfigFile().Routes = %+v, want nil", got.Routes)
	}
	if !strings.Contains(err.Error(), "load config file") {
		t.Errorf("loadConfigFile() error = %q, want load context", err)
	}
	if !strings.Contains(err.Error(), "decode YAML config") {
		t.Errorf("loadConfigFile() error = %q, want decode context", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("loadConfigFile() error = %q, want path %q", err, path)
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "devgate.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	return path
}
