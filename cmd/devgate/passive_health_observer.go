package main

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/chyioishi/devgate/internal/upstream"
)

type passiveHealthMetricsObserver interface {
	RecordEjection(url.URL)
	RecordRecovery(url.URL)
}

type routePassiveHealthObserver struct {
	routeName       string
	picker          *upstream.HealthAwarePicker
	tracker         *upstream.PassiveHealthTracker
	wake            chan struct{}
	logger          *slog.Logger
	metricsObserver passiveHealthMetricsObserver
}

var _ upstream.PassiveHealthObserver = (*routePassiveHealthObserver)(nil)
var _ healthRunner = (*routePassiveHealthObserver)(nil)

func newRoutePassiveHealthObserver(
	routeName string,
	tracker *upstream.PassiveHealthTracker,
	picker *upstream.HealthAwarePicker,
	logger *slog.Logger,
	metricsObserver passiveHealthMetricsObserver,
) (*routePassiveHealthObserver, error) {
	if tracker == nil {
		return nil, errors.New("tracker was not provided")
	}
	if picker == nil {
		return nil, errors.New("picker was not provided")
	}
	if logger == nil {
		return nil, errors.New("logger was not provided")
	}
	if metricsObserver == nil {
		return nil, errors.New("metrics observer was not provided")
	}
	return &routePassiveHealthObserver{
		routeName:       routeName,
		picker:          picker,
		tracker:         tracker,
		wake:            make(chan struct{}, 1),
		logger:          logger,
		metricsObserver: metricsObserver,
	}, nil
}

func (r *routePassiveHealthObserver) Run(ctx context.Context) {
	for {
		delay, pending := r.tracker.NextRecoveryDelay()

		if !pending {
			select {
			case <-ctx.Done():
				return
			case <-r.wake:
				continue
			}
		}

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-r.wake:
			timer.Stop()
			continue
		case <-timer.C:
			healthyEndpoints := r.tracker.HealthyEndpoints()
			for _, endpoint := range healthyEndpoints {
				r.metricsObserver.RecordRecovery(endpoint)
			}
			if err := r.picker.Refresh(); err != nil {
				r.logger.Error(
					"failed to refresh passively recovered upstream picker",
					"route", r.routeName,
					"error", err,
				)
			}
		}
	}
}

func (r *routePassiveHealthObserver) RecordEjection(target url.URL) {
	r.metricsObserver.RecordEjection(target)
	r.logger.Warn(
		"upstream endpoint passively ejected",
		"route", r.routeName,
		"target", target.Redacted(),
	)
	err := r.picker.Refresh()
	if err != nil {
		r.logger.Error(
			"failed to refresh healthy upstream picker",
			"route", r.routeName,
			"target", target.Redacted(),
			"error", err,
		)
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
