package main

import (
	"errors"
	"log/slog"
	"net/url"

	"github.com/chyioishi/devgate/internal/discovery"
)

type routeDNSRefreshObserver struct {
	routeName       string
	metricsObserver discovery.DNSRefreshObserver
	logger          *slog.Logger
}

var _ discovery.DNSRefreshObserver = (*routeDNSRefreshObserver)(nil)

func newRouteDNSRefreshObserver(
	routeName string,
	metricsObserver discovery.DNSRefreshObserver,
	logger *slog.Logger,
) (*routeDNSRefreshObserver, error) {
	if metricsObserver == nil {
		return nil, errors.New("metrics observer was not provided")
	}
	if logger == nil {
		return nil, errors.New("logger was not provided")
	}
	return &routeDNSRefreshObserver{
		routeName:       routeName,
		metricsObserver: metricsObserver,
		logger:          logger,
	}, nil
}

func (o *routeDNSRefreshObserver) RecordRefresh(
	target url.URL,
	endpointsCount int,
	err error,
) {
	o.metricsObserver.RecordRefresh(target, endpointsCount, err)
	if err != nil {
		o.logger.Error(
			"DNS endpoint refresh failed",
			"route", o.routeName,
			"target", target.Redacted(),
			"error", err,
		)
		return
	}
	o.logger.Debug(
		"DNS endpoints refreshed",
		"route", o.routeName,
		"target", target.Redacted(),
		"endpoint_count", endpointsCount,
	)
}
