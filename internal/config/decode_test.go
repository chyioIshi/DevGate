package config

import (
	"slices"
	"strings"
	"testing"
)

func TestDecodeConfig(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
  - name: fallback
    protocol: http
    path_prefix: /
    upstream_url: http://frontend-service:8080
`
	want := []RouteConfig{
		{
			Name:        "users",
			Protocol:    "http",
			PathPrefix:  "/api/users",
			UpstreamURL: "http://users-service:8080",
		},
		{
			Name:        "fallback",
			Protocol:    "http",
			PathPrefix:  "/",
			UpstreamURL: "http://frontend-service:8080",
		},
	}

	got, err := decodeConfig(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	if !slices.Equal(got.Routes, want) {
		t.Errorf("decodeConfig().Routes = %+v, want %+v", got.Routes, want)
	}
}

func TestDecodeConfigRejectsUnknownField(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    unknown_field: value
`

	got, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() error = nil, want unknown field error")
	}
	if got.Routes != nil {
		t.Errorf("decodeConfig().Routes = %+v, want nil", got.Routes)
	}
	if !strings.Contains(err.Error(), "decode YAML config") {
		t.Errorf("decodeConfig() error = %q, want decoding context", err)
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("decodeConfig() error = %q, want unknown field context", err)
	}
}

func TestDecodeConfigRejectsMalformedYAML(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: [http
`

	got, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() error = nil, want YAML syntax error")
	}
	if got.Routes != nil {
		t.Errorf("decodeConfig().Routes = %+v, want nil", got.Routes)
	}
	if !strings.Contains(err.Error(), "decode YAML config") {
		t.Errorf("decodeConfig() error = %q, want decoding context", err)
	}
}

func TestDecodeConfigReturnsZeroValueAfterPartialDecode(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
  - name: fallback
    protocol: http
    path_prefix: /
    upstream_url: http://frontend-service:8080
    unknown_field: value
`

	got, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() error = nil, want decoding error")
	}
	if got.Routes != nil {
		t.Errorf("decodeConfig().Routes = %+v, want nil", got.Routes)
	}
}
