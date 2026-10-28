package main

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/discovery"
)

func TestNewRouteDNSRefreshObserverRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		metrics     discovery.DNSRefreshObserver
		logger      *slog.Logger
		wantMessage string
	}{
		{
			name:        "missing metrics observer",
			logger:      slog.New(slog.DiscardHandler),
			wantMessage: "metrics observer",
		},
		{
			name:        "missing logger",
			metrics:     &recordingDNSRefreshObserver{},
			wantMessage: "logger",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observer, err := newRouteDNSRefreshObserver(
				"users",
				test.metrics,
				test.logger,
			)
			if err == nil {
				t.Fatal("newRouteDNSRefreshObserver() error = nil, want an error")
			}
			if observer != nil {
				t.Errorf("newRouteDNSRefreshObserver() observer = %#v, want nil", observer)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf(
					"newRouteDNSRefreshObserver() error = %q, want %q",
					err,
					test.wantMessage,
				)
			}
		})
	}
}

func TestRouteDNSRefreshObserverLogsSuccessfulRefresh(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(
		&logs,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	))
	metricsObserver := &recordingDNSRefreshObserver{}
	observer, err := newRouteDNSRefreshObserver("users", metricsObserver, logger)
	if err != nil {
		t.Fatalf("newRouteDNSRefreshObserver() error = %v", err)
	}
	target := mustParseObserverTarget(
		t,
		"http://user:secret@users.service.local:8080/api",
	)

	observer.RecordRefresh(target, 2, nil)
	assertRecordedDNSRefresh(t, metricsObserver, target, 2, nil)

	records := decodeObserverLogs(t, logs.Bytes())
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1", len(records))
	}
	record := records[0]
	if got := record["level"]; got != "DEBUG" {
		t.Errorf("log level = %v, want %q", got, "DEBUG")
	}
	if got := record["msg"]; got != "DNS endpoints refreshed" {
		t.Errorf("log message = %v, want successful refresh message", got)
	}
	if got := record["route"]; got != "users" {
		t.Errorf("log route = %v, want %q", got, "users")
	}
	if got := record["target"]; got != target.Redacted() {
		t.Errorf("log target = %v, want %q", got, target.Redacted())
	}
	if got := record["endpoint_count"]; got != float64(2) {
		t.Errorf("log endpoint count = %v, want 2", got)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs expose target password: %s", logs.String())
	}
}

func TestRouteDNSRefreshObserverLogsFailedRefresh(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	metricsObserver := &recordingDNSRefreshObserver{}
	observer, err := newRouteDNSRefreshObserver(
		"users",
		metricsObserver,
		slog.New(slog.NewJSONHandler(&logs, nil)),
	)
	if err != nil {
		t.Fatalf("newRouteDNSRefreshObserver() error = %v", err)
	}
	target := mustParseObserverTarget(
		t,
		"http://user:secret@users.service.local:8080/api",
	)
	refreshErr := errors.New("lookup failed")

	observer.RecordRefresh(target, 0, refreshErr)
	assertRecordedDNSRefresh(t, metricsObserver, target, 0, refreshErr)

	records := decodeObserverLogs(t, logs.Bytes())
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1", len(records))
	}
	record := records[0]
	if got := record["level"]; got != "ERROR" {
		t.Errorf("log level = %v, want %q", got, "ERROR")
	}
	if got := record["msg"]; got != "DNS endpoint refresh failed" {
		t.Errorf("log message = %v, want failed refresh message", got)
	}
	if got := record["route"]; got != "users" {
		t.Errorf("log route = %v, want %q", got, "users")
	}
	if got := record["target"]; got != target.Redacted() {
		t.Errorf("log target = %v, want %q", got, target.Redacted())
	}
	if got := record["error"]; got != refreshErr.Error() {
		t.Errorf("log error = %v, want %q", got, refreshErr)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs expose target password: %s", logs.String())
	}
}

type recordingDNSRefreshObserver struct {
	calls         int
	target        url.URL
	endpointCount int
	err           error
}

func (o *recordingDNSRefreshObserver) RecordRefresh(
	target url.URL,
	endpointCount int,
	err error,
) {
	o.calls++
	o.target = target
	o.endpointCount = endpointCount
	o.err = err
}

func assertRecordedDNSRefresh(
	t *testing.T,
	observer *recordingDNSRefreshObserver,
	target url.URL,
	endpointCount int,
	err error,
) {
	t.Helper()

	if observer.calls != 1 {
		t.Fatalf("metrics observer calls = %d, want 1", observer.calls)
	}
	if observer.target != target {
		t.Errorf("metrics observer target = %v, want %v", observer.target, target)
	}
	if observer.endpointCount != endpointCount {
		t.Errorf(
			"metrics observer endpoint count = %d, want %d",
			observer.endpointCount,
			endpointCount,
		)
	}
	if !errors.Is(observer.err, err) {
		t.Errorf("metrics observer error = %v, want %v", observer.err, err)
	}
}
