package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/proxytarget"
	"github.com/chyioishi/devgate/internal/upstream"
)

func TestRoutePassiveHealthObserverRefreshesPickerOnEjection(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://user:secret@upstream.example")
	source := &passiveObserverSource{endpoints: []url.URL{target}}
	base := &observerTestPicker{}
	picker, err := upstream.NewHealthAwarePicker(base, source)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	tracker, err := upstream.NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	var logs bytes.Buffer
	observer, err := newRoutePassiveHealthObserver(
		"users",
		tracker,
		picker,
		slog.New(slog.NewJSONHandler(&logs, nil)),
	)
	if err != nil {
		t.Fatalf("newRoutePassiveHealthObserver() error = %v", err)
	}
	source.endpoints = nil

	observer.RecordEjection(target)

	_, release, err := picker.Acquire()
	if !errors.Is(err, upstream.ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}
	records := decodeObserverLogs(t, logs.Bytes())
	if len(records) != 1 {
		t.Fatalf("log record count = %d, want 1", len(records))
	}
	if got := records[0]["level"]; got != "WARN" {
		t.Errorf("log level = %v, want %q", got, "WARN")
	}
	if got := records[0]["msg"]; got != "upstream endpoint passively ejected" {
		t.Errorf("log message = %v, want passive ejection message", got)
	}
	if got := records[0]["route"]; got != "users" {
		t.Errorf("log route = %v, want %q", got, "users")
	}
	if got := records[0]["target"]; got != target.Redacted() {
		t.Errorf("log target = %v, want %q", got, target.Redacted())
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs expose target password: %s", logs.String())
	}
}

func TestRoutePassiveHealthObserverLogsRefreshFailure(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://user:secret@upstream.example")
	source := &passiveObserverSource{endpoints: []url.URL{target}}
	base := &observerTestPicker{}
	picker, err := upstream.NewHealthAwarePicker(base, source)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	tracker, err := upstream.NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	base.replaceErr = errors.New("replace failed")
	var logs bytes.Buffer
	observer, err := newRoutePassiveHealthObserver(
		"users",
		tracker,
		picker,
		slog.New(slog.NewJSONHandler(&logs, nil)),
	)
	if err != nil {
		t.Fatalf("newRoutePassiveHealthObserver() error = %v", err)
	}

	observer.RecordEjection(target)

	records := decodeObserverLogs(t, logs.Bytes())
	if len(records) != 2 {
		t.Fatalf("log record count = %d, want 2", len(records))
	}
	errorRecord := records[1]
	if got := errorRecord["level"]; got != "ERROR" {
		t.Errorf("error log level = %v, want %q", got, "ERROR")
	}
	if got := errorRecord["msg"]; got != "failed to refresh healthy upstream picker" {
		t.Errorf("error log message = %v, want refresh failure message", got)
	}
	if got := errorRecord["route"]; got != "users" {
		t.Errorf("error log route = %v, want %q", got, "users")
	}
	if got := errorRecord["target"]; got != target.Redacted() {
		t.Errorf("error log target = %v, want %q", got, target.Redacted())
	}
	if got := errorRecord["error"]; got != "replace healthy endpoints: replace failed" {
		t.Errorf("error log error = %v, want wrapped replace error", got)
	}
	if strings.Contains(logs.String(), "secret") {
		t.Errorf("logs expose target password: %s", logs.String())
	}
}

func TestNewRoutePassiveHealthObserverRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://upstream.example")
	tracker, err := upstream.NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	source := &passiveObserverSource{endpoints: []url.URL{target}}
	base := &observerTestPicker{}
	picker, err := upstream.NewHealthAwarePicker(base, source)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	tests := []struct {
		name        string
		tracker     *upstream.PassiveHealthTracker
		picker      *upstream.HealthAwarePicker
		logger      *slog.Logger
		wantMessage string
	}{
		{
			name:        "missing tracker",
			picker:      picker,
			logger:      logger,
			wantMessage: "tracker was not provided",
		},
		{
			name:        "missing picker",
			tracker:     tracker,
			logger:      logger,
			wantMessage: "picker was not provided",
		},
		{
			name:        "missing logger",
			tracker:     tracker,
			picker:      picker,
			wantMessage: "logger was not provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := newRoutePassiveHealthObserver(
				"users",
				test.tracker,
				test.picker,
				test.logger,
			)
			if err == nil {
				t.Fatalf("newRoutePassiveHealthObserver() error = nil, want %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("newRoutePassiveHealthObserver() observer = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("newRoutePassiveHealthObserver() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestRoutePassiveHealthObserverCoalescesRecoveryWakeSignals(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://upstream.example")
	tracker, err := upstream.NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	source := &passiveObserverSource{}
	base := &observerTestPicker{}
	picker, err := upstream.NewHealthAwarePicker(base, source)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	observer, err := newRoutePassiveHealthObserver(
		"users",
		tracker,
		picker,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("newRoutePassiveHealthObserver() error = %v", err)
	}

	observer.RecordEjection(target)
	observer.RecordEjection(target)

	if got := len(observer.wake); got != 1 {
		t.Errorf("pending recovery wake signals = %d, want 1", got)
	}
	if got := cap(observer.wake); got != 1 {
		t.Errorf("recovery wake channel capacity = %d, want 1", got)
	}
}

func TestRoutePassiveHealthObserverRecoversEndpointAndStops(t *testing.T) {
	t.Parallel()

	target := mustParseObserverTarget(t, "http://upstream.example")
	tracker, err := upstream.NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		200*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	basePicker, err := upstream.NewRoundRobin([]url.URL{target})
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	picker, err := upstream.NewHealthAwarePicker(basePicker, tracker)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	observer, err := newRoutePassiveHealthObserver(
		"users",
		tracker,
		picker,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("newRoutePassiveHealthObserver() error = %v", err)
	}
	baseTransport := passiveObserverRoundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       http.NoBody,
			}, nil
		},
	)
	transport, err := upstream.NewPassiveHealthTransport(
		baseTransport,
		tracker,
		observer,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		observer.Run(ctx)
		close(runDone)
	}()

	requestCtx := proxytarget.WithTarget(context.Background(), target)
	response, err := transport.RoundTrip((&http.Request{}).WithContext(requestCtx))
	if err != nil {
		cancel()
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		cancel()
		t.Fatalf("close response body: %v", closeErr)
	}
	_, release, err := picker.Acquire()
	if !errors.Is(err, upstream.ErrNoHealthyEndpoints) {
		cancel()
		t.Fatalf("Acquire() during ejection error = %v, want ErrNoHealthyEndpoints", err)
	}
	if release != nil {
		cancel()
		t.Fatal("Acquire() during ejection release is non-nil, want nil")
	}

	deadline := time.NewTimer(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		_, release, err = picker.Acquire()
		if err == nil {
			release()
			break
		}
		if !errors.Is(err, upstream.ErrNoHealthyEndpoints) {
			cancel()
			t.Fatalf("Acquire() while awaiting recovery error = %v", err)
		}
		select {
		case <-deadline.C:
			cancel()
			t.Fatal("endpoint was not restored before deadline")
		case <-ticker.C:
		}
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}

type passiveObserverSource struct {
	endpoints []url.URL
}

func (s *passiveObserverSource) HealthyEndpoints() []url.URL {
	return s.endpoints
}

type passiveObserverRoundTripFunc func(*http.Request) (*http.Response, error)

func (f passiveObserverRoundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return f(request)
}
