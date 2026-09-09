package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

func TestHandlerServesAllRegisteredMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	httpMetrics := metrics.NewHTTP(registry)
	httpMetrics.RequestStarted()
	httpMetrics.RequestFinished("users", http.StatusCreated, 250*time.Millisecond)
	extraMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_extra_value",
		Help: "A metric registered independently of HTTP metrics.",
	})
	registry.MustRegister(extraMetric)
	extraMetric.Set(7)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	metrics.Handler(registry).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("content type = %q, want text/plain", contentType)
	}
	for _, want := range []string{
		`devgate_http_requests_total{route="users",status="201"} 1`,
		`devgate_http_requests_in_flight 0`,
		`devgate_http_request_duration_seconds_count{route="users"} 1`,
		`devgate_http_request_duration_seconds_sum{route="users"} 0.25`,
		`test_extra_value 7`,
	} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("response body does not contain %q", want)
		}
	}
}
