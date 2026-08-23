package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "devgate"
	subsystem = "http"

	counterName   = "requests_total"
	histogramName = "request_duration_seconds"
	gaugeName     = "requests_in_flight"
)

// HTTP is a metrics collector for HTTP requests.
type HTTP struct {
	registry         *prometheus.Registry
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	requestsInFlight prometheus.Gauge
}

// NewHTTP creates a new HTTP metrics collector.
func NewHTTP() *HTTP {
	registry := prometheus.NewRegistry()
	counterOpts := prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      counterName,
		Help:      "Total number of HTTP requests handled by the gateway.",
	}
	histogramVecOpts := prometheus.HistogramOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      histogramName,
		Help:      "Duration of HTTP requests handled by the gateway in seconds.",
	}
	gaugeOpts := prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystem,
		Name:      gaugeName,
		Help:      "Number of HTTP requests currently in flight.",
	}
	counterCollector := prometheus.NewCounterVec(counterOpts, []string{"route", "status"})
	histogramCollector := prometheus.NewHistogramVec(histogramVecOpts, []string{"route"})
	gaugeCollector := prometheus.NewGauge(gaugeOpts)

	registry.MustRegister(counterCollector, histogramCollector, gaugeCollector)

	return &HTTP{
		registry:         registry,
		requestsTotal:    counterCollector,
		requestDuration:  histogramCollector,
		requestsInFlight: gaugeCollector,
	}
}

// RequestStarted records an HTTP request entering the gateway.
func (m *HTTP) RequestStarted() {
	m.requestsInFlight.Inc()
}

// RequestFinished records the result and duration of a completed HTTP request.
func (m *HTTP) RequestFinished(
	route string,
	status int,
	duration time.Duration,
) {
	stringStatus := strconv.Itoa(status)
	m.requestsInFlight.Dec()
	m.requestsTotal.WithLabelValues(route, stringStatus).Inc()
	m.requestDuration.WithLabelValues(route).Observe(duration.Seconds())
}
