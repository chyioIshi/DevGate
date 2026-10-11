package main

import (
	"log/slog"
	"net/url"

	"github.com/chyioishi/devgate/internal/upstream"
)

type routeHealthCheckObserver struct {
	routeName       string
	picker          *upstream.HealthAwarePicker
	logger          *slog.Logger
	metricsObserver upstream.HealthCheckObserver
}

var _ upstream.HealthCheckObserver = (*routeHealthCheckObserver)(nil)

func (r *routeHealthCheckObserver) RecordProbeResult(target url.URL, successful bool, err error) {
	r.metricsObserver.RecordProbeResult(target, successful, err)
}

// RecordStateTransition records a completed transition to healthy or unhealthy.
func (r *routeHealthCheckObserver) RecordStateTransition(target url.URL, healthy bool) {
	r.metricsObserver.RecordStateTransition(target, healthy)

	err := r.picker.Refresh()
	r.logger.Info("upstream health state changed",
		"route", r.routeName,
		"target", target.Redacted(),
		"healthy", healthy,
	)
	if err != nil {
		r.logger.Error(
			"failed to refresh healthy upstream picker",
			"route", r.routeName,
			"target", target.Redacted(),
			"error", err)
	}
}
