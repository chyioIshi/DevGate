package config

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodeConfig(t *testing.T) {
	input := `
error_responses:
  404:
    body: '{"error":"not found"}'
    headers:
      Content-Type: application/json
  500:
    body: '{"error":"internal server error"}'
    headers:
      Content-Type: application/json
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    methods:
      - GET
      - POST
    hosts:
      - api.example.com
      - api.internal
    priority: 100
    header_matches:
      - name: X-Environment
        exact: production
      - name: X-API-Version
        exact: v2
    upstream:
      load_balancing: round_robin
      discovery:
        static:
          endpoints:
            - url: http://users-1:8080
            - url: http://users-2:8080
    strip_path_prefix: true
    request_timeout: 2.5s
    max_request_body_bytes: 10485760
    request_headers:
      set:
        X-Gateway: DevGate
        X-Environment: production
      remove:
        - X-Legacy-Header
    response_headers:
      set:
        X-Gateway-Response: DevGate
      remove:
        - X-Legacy-Response-Header
    error_responses:
      429:
        body: '{"error":"rate limit exceeded"}'
        headers:
          Content-Type: application/json
      502:
        body: '{"error":"upstream unavailable"}'
        headers:
          Content-Type: application/json
    rate_limit:
      requests_per_second: 10.5
      burst: 20
  - name: fallback
    protocol: http
    path_prefix: /
    upstream_url: http://frontend-service:8080
  - name: health
    protocol: http
    path_exact: /healthz
    priority: -10
    upstream_url: http://health-service:8080
  - name: maintenance
    path_prefix: /api
    direct_response:
      status: 503
      body: service temporarily unavailable
  - name: legacy-api
    path_exact: /legacy
    redirect:
      status: 308
      location: /api/v2
`
	want := []RouteConfig{
		{
			Name:       "users",
			Protocol:   "http",
			PathPrefix: "/api/users",
			Methods:    []string{"GET", "POST"},
			Hosts:      []string{"api.example.com", "api.internal"},
			Priority:   100,
			HeaderMatches: []HeaderMatchConfig{
				{Name: "X-Environment", Exact: "production"},
				{Name: "X-API-Version", Exact: "v2"},
			},
			Upstream: &UpstreamConfig{
				LoadBalancing: LoadBalancingPolicyRoundRobin,
				Discovery: &UpstreamDiscoveryConfig{
					Static: &StaticUpstreamDiscoveryConfig{
						Endpoints: []UpstreamEndpointConfig{
							{URL: "http://users-1:8080"},
							{URL: "http://users-2:8080"},
						},
					},
				},
			},
			StripPathPrefix:     true,
			RequestTimeout:      2500 * time.Millisecond,
			MaxRequestBodyBytes: 10 * 1024 * 1024,
			RequestHeaders: &HeaderTransformConfig{
				Set: map[string]string{
					"X-Gateway":     "DevGate",
					"X-Environment": "production",
				},
				Remove: []string{"X-Legacy-Header"},
			},
			ResponseHeaders: &HeaderTransformConfig{
				Set:    map[string]string{"X-Gateway-Response": "DevGate"},
				Remove: []string{"X-Legacy-Response-Header"},
			},
			ErrorResponses: map[int]ErrorResponseConfig{
				http.StatusTooManyRequests: {
					Body:    `{"error":"rate limit exceeded"}`,
					Headers: map[string]string{"Content-Type": "application/json"},
				},
				http.StatusBadGateway: {
					Body:    `{"error":"upstream unavailable"}`,
					Headers: map[string]string{"Content-Type": "application/json"},
				},
			},
			RateLimit: &RateLimitConfig{
				RequestsPerSecond: 10.5,
				Burst:             20,
			},
		},
		{
			Name:        "fallback",
			Protocol:    "http",
			PathPrefix:  "/",
			UpstreamURL: "http://frontend-service:8080",
		},
		{
			Name:        "health",
			Protocol:    "http",
			PathExact:   "/healthz",
			Priority:    -10,
			UpstreamURL: "http://health-service:8080",
		},
		{
			Name:       "maintenance",
			PathPrefix: "/api",
			DirectResponse: &DirectResponseConfig{
				StatusCode: 503,
				Body:       "service temporarily unavailable",
			},
		},
		{
			Name:      "legacy-api",
			PathExact: "/legacy",
			Redirect: &RedirectConfig{
				StatusCode: 308,
				Location:   "/api/v2",
			},
		},
	}
	wantErrorResponses := map[int]ErrorResponseConfig{
		http.StatusNotFound: {
			Body:    `{"error":"not found"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
		http.StatusInternalServerError: {
			Body:    `{"error":"internal server error"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
	}

	got, err := decodeConfig(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	if !reflect.DeepEqual(got.Routes, want) {
		t.Errorf("decodeConfig().Routes = %+v, want %+v", got.Routes, want)
	}
	if !reflect.DeepEqual(got.ErrorResponses, wantErrorResponses) {
		t.Errorf(
			"decodeConfig().ErrorResponses = %+v, want %+v",
			got.ErrorResponses,
			wantErrorResponses,
		)
	}
}

func TestDecodeConfigRandomLoadBalancingPolicy(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /users
    upstream:
      load_balancing: random
      discovery:
        static:
          endpoints:
            - url: http://users-1:8080
            - url: http://users-2:8080
`

	got, err := decodeConfig(strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}
	if len(got.Routes) != 1 {
		t.Fatalf("decodeConfig() route count = %d, want 1", len(got.Routes))
	}
	if got.Routes[0].Upstream == nil {
		t.Fatal("decodeConfig() upstream = nil, want non-nil")
	}
	if policy := got.Routes[0].Upstream.LoadBalancing; policy != LoadBalancingPolicyRandom {
		t.Errorf("decodeConfig() policy = %q, want %q", policy, LoadBalancingPolicyRandom)
	}
}

func TestDecodeConfigRejectsUnknownUpstreamFields(t *testing.T) {
	tests := []struct {
		name         string
		upstreamYAML string
		unknownField string
	}{
		{
			name: "upstream",
			upstreamYAML: `
      load_balancing: round_robin
      unknown_upstream: value`,
			unknownField: "unknown_upstream",
		},
		{
			name: "discovery",
			upstreamYAML: `
      load_balancing: round_robin
      discovery:
        unknown_discovery: {}`,
			unknownField: "unknown_discovery",
		},
		{
			name: "static discovery",
			upstreamYAML: `
      load_balancing: round_robin
      discovery:
        static:
          unknown_static: value`,
			unknownField: "unknown_static",
		},
		{
			name: "endpoint",
			upstreamYAML: `
      load_balancing: round_robin
      discovery:
        static:
          endpoints:
            - url: http://users:8080
              unknown_endpoint: value`,
			unknownField: "unknown_endpoint",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream:` + test.upstreamYAML

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
			if !strings.Contains(err.Error(), `unknown field "`+test.unknownField+`"`) {
				t.Errorf(
					"decodeConfig() error = %q, want unknown field %q",
					err,
					test.unknownField,
				)
			}
		})
	}
}

func TestDecodeConfigRejectsInvalidRequestTimeout(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    request_timeout: fast
`

	got, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() error = nil, want invalid duration error")
	}
	if got.Routes != nil {
		t.Errorf("decodeConfig().Routes = %+v, want nil", got.Routes)
	}
	if !strings.Contains(err.Error(), "decode YAML config") {
		t.Errorf("decodeConfig() error = %q, want decoding context", err)
	}
	if !strings.Contains(err.Error(), "invalid duration") {
		t.Errorf("decodeConfig() error = %q, want invalid duration context", err)
	}
}

func TestDecodeConfigRejectsUnknownRateLimitField(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    rate_limit:
      requests_per_second: 10
      burst: 20
      unknown_field: value
`

	got, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() error = nil, want unknown field error")
	}
	if got.Routes != nil {
		t.Errorf("decodeConfig().Routes = %+v, want nil", got.Routes)
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("decodeConfig() error = %q, want unknown field context", err)
	}
}

func TestDecodeConfigRejectsUnknownRequestHeadersField(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    request_headers:
      set:
        X-Gateway: DevGate
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

func TestDecodeConfigRejectsUnknownResponseHeadersField(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    response_headers:
      set:
        X-Gateway-Response: DevGate
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

func TestDecodeConfigRejectsUnknownDirectResponseField(t *testing.T) {
	input := `
routes:
  - name: maintenance
    path_prefix: /api
    direct_response:
      status: 503
      payload: service temporarily unavailable
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
	if !strings.Contains(err.Error(), `unknown field "payload"`) {
		t.Errorf("decodeConfig() error = %q, want unknown field context", err)
	}
}

func TestDecodeConfigRejectsUnknownRedirectField(t *testing.T) {
	input := `
routes:
  - name: legacy-api
    path_exact: /legacy
    redirect:
      status: 308
      target: /api/v2
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
	if !strings.Contains(err.Error(), `unknown field "target"`) {
		t.Errorf("decodeConfig() error = %q, want unknown field context", err)
	}
}

func TestDecodeConfigRejectsUnknownErrorResponseField(t *testing.T) {
	input := `
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
    error_responses:
      502:
        body: upstream unavailable
        template: error.html
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
	if !strings.Contains(err.Error(), `unknown field "template"`) {
		t.Errorf("decodeConfig() error = %q, want unknown field context", err)
	}
}

func TestDecodeConfigRejectsUnknownGlobalErrorResponseField(t *testing.T) {
	input := `
error_responses:
  404:
    body: not found
    template: error.html
routes:
  - name: users
    protocol: http
    path_prefix: /api/users
    upstream_url: http://users-service:8080
`

	got, err := decodeConfig(strings.NewReader(input))
	if err == nil {
		t.Fatal("decodeConfig() error = nil, want unknown field error")
	}
	if got.Routes != nil {
		t.Errorf("decodeConfig().Routes = %+v, want nil", got.Routes)
	}
	if got.ErrorResponses != nil {
		t.Errorf("decodeConfig().ErrorResponses = %+v, want nil", got.ErrorResponses)
	}
	if !strings.Contains(err.Error(), "decode YAML config") {
		t.Errorf("decodeConfig() error = %q, want decoding context", err)
	}
	if !strings.Contains(err.Error(), `unknown field "template"`) {
		t.Errorf("decodeConfig() error = %q, want unknown field context", err)
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
