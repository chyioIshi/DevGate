package metrics

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewHTTPRegistersCollectors(t *testing.T) {
	registry := prometheus.NewRegistry()
	httpMetrics := NewHTTP(registry)
	httpMetrics.requestsTotal.WithLabelValues("users", "200")
	httpMetrics.requestDuration.WithLabelValues("users").Observe(0)

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	wantTypes := map[string]string{
		"devgate_http_requests_total":           "COUNTER",
		"devgate_http_request_duration_seconds": "HISTOGRAM",
		"devgate_http_requests_in_flight":       "GAUGE",
	}
	for _, family := range metricFamilies {
		name := family.GetName()
		wantType, exists := wantTypes[name]
		if !exists {
			t.Errorf("unexpected metric family %q", name)
			continue
		}
		if gotType := family.GetType().String(); gotType != wantType {
			t.Errorf("metric family %q type = %s, want %s", name, gotType, wantType)
		}
		delete(wantTypes, name)
	}

	for name := range wantTypes {
		t.Errorf("metric family %q was not registered", name)
	}
}

func TestNewHTTPKeepsRegistriesIsolated(t *testing.T) {
	firstRegistry := prometheus.NewRegistry()
	secondRegistry := prometheus.NewRegistry()
	first := NewHTTP(firstRegistry)
	NewHTTP(secondRegistry)
	first.RequestStarted()

	for _, test := range []struct {
		name     string
		registry *prometheus.Registry
		want     string
	}{
		{name: "active instance", registry: firstRegistry, want: "1"},
		{name: "idle instance", registry: secondRegistry, want: "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			want := "# HELP devgate_http_requests_in_flight Number of HTTP requests currently in flight.\n" +
				"# TYPE devgate_http_requests_in_flight gauge\n" +
				"devgate_http_requests_in_flight " + test.want + "\n"
			if err := testutil.GatherAndCompare(
				test.registry,
				strings.NewReader(want),
				"devgate_http_requests_in_flight",
			); err != nil {
				t.Errorf("registry metrics mismatch: %v", err)
			}
		})
	}
}

func TestHTTPRecordsRequestLifecycle(t *testing.T) {
	registry := prometheus.NewRegistry()
	httpMetrics := NewHTTP(registry)

	httpMetrics.RequestStarted()
	if got, want := testutil.ToFloat64(httpMetrics.requestsInFlight), float64(1); got != want {
		t.Errorf("requests in flight after start = %v, want %v", got, want)
	}

	httpMetrics.RequestFinished("users", http.StatusCreated, 250*time.Millisecond)

	if got, want := testutil.ToFloat64(httpMetrics.requestsInFlight), float64(0); got != want {
		t.Errorf("requests in flight after finish = %v, want %v", got, want)
	}
	requestsTotal := httpMetrics.requestsTotal.WithLabelValues("users", "201")
	if got, want := testutil.ToFloat64(requestsTotal), float64(1); got != want {
		t.Errorf("requests total = %v, want %v", got, want)
	}

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() != "devgate_http_request_duration_seconds" {
			continue
		}
		if got, want := len(family.GetMetric()), 1; got != want {
			t.Fatalf("request duration metric count = %d, want %d", got, want)
		}

		histogram := family.GetMetric()[0].GetHistogram()
		if got, want := histogram.GetSampleCount(), uint64(1); got != want {
			t.Errorf("request duration sample count = %d, want %d", got, want)
		}
		if got, want := histogram.GetSampleSum(), 0.25; got != want {
			t.Errorf("request duration sample sum = %v, want %v", got, want)
		}
		return
	}

	t.Error("request duration metric was not gathered")
}
