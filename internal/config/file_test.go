package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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

	if !reflect.DeepEqual(got.Routes, want) {
		t.Errorf("loadConfigFile().Routes = %+v, want %+v", got.Routes, want)
	}
}

func TestLoadExampleConfig(t *testing.T) {
	path := filepath.Join("..", "..", "devgate.example.yaml")

	got, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("loadConfigFile(%q) error = %v", path, err)
	}
	if len(got.Routes) == 0 {
		t.Fatal("example config routes are empty")
	}

	wantHeaderMatches := []HeaderMatchConfig{
		{Name: "X-Environment", Exact: "production"},
	}
	if !reflect.DeepEqual(got.Routes[0].HeaderMatches, wantHeaderMatches) {
		t.Errorf(
			"example config header matches = %+v, want %+v",
			got.Routes[0].HeaderMatches,
			wantHeaderMatches,
		)
	}
	if len(got.Routes) < 2 {
		t.Fatalf("example config routes length = %d, want at least 2", len(got.Routes))
	}
	if got.Routes[1].PathExact != "/api/status" {
		t.Errorf("example config exact path = %q, want %q", got.Routes[1].PathExact, "/api/status")
	}

	requestHeaders := got.Routes[0].RequestHeaders
	if requestHeaders == nil {
		t.Fatal("example config request headers = nil, want configured policy")
	}
	if value := requestHeaders.Set["X-Gateway"]; value != "DevGate" {
		t.Errorf("example config X-Gateway = %q, want %q", value, "DevGate")
	}
	if !slices.Equal(requestHeaders.Remove, []string{"X-Legacy-Header"}) {
		t.Errorf(
			"example config removed headers = %q, want %q",
			requestHeaders.Remove,
			[]string{"X-Legacy-Header"},
		)
	}

	responseHeaders := got.Routes[0].ResponseHeaders
	if responseHeaders == nil {
		t.Fatal("example config response headers = nil, want configured policy")
	}
	if value := responseHeaders.Set["X-Gateway-Response"]; value != "DevGate" {
		t.Errorf("example config X-Gateway-Response = %q, want %q", value, "DevGate")
	}
	if !slices.Equal(responseHeaders.Remove, []string{"X-Legacy-Response-Header"}) {
		t.Errorf(
			"example config removed response headers = %q, want %q",
			responseHeaders.Remove,
			[]string{"X-Legacy-Response-Header"},
		)
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
