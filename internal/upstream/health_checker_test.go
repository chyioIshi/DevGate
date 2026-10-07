package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHealthCheckerStoresConfiguration(t *testing.T) {
	t.Parallel()

	client := &http.Client{}
	tracker := newTestHealthTracker(t)
	observer := &recordingHealthCheckObserver{}
	checker, err := NewHealthChecker(
		client,
		tracker,
		5*time.Second,
		time.Second,
		"/healthz",
		2,
		observer,
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}

	if checker.client != client {
		t.Error("client was not preserved")
	}
	if checker.tracker != tracker {
		t.Error("tracker was not preserved")
	}
	if checker.interval != 5*time.Second {
		t.Errorf("interval = %s, want %s", checker.interval, 5*time.Second)
	}
	if checker.timeout != time.Second {
		t.Errorf("timeout = %s, want %s", checker.timeout, time.Second)
	}
	if checker.healthPath != "/healthz" {
		t.Errorf("health path = %q, want %q", checker.healthPath, "/healthz")
	}
	if checker.maxConcurrentProbes != 2 {
		t.Errorf("max concurrent probes = %d, want 2", checker.maxConcurrentProbes)
	}
	if checker.observer != observer {
		t.Error("observer was not preserved")
	}
}

func TestNewHealthCheckerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	client := &http.Client{}
	tracker := newTestHealthTracker(t)
	tests := []struct {
		name        string
		client      *http.Client
		tracker     *HealthTracker
		interval    time.Duration
		timeout     time.Duration
		healthPath  string
		maxProbes   int
		observer    HealthCheckObserver
		wantMessage string
	}{
		{
			name:        "nil client",
			tracker:     tracker,
			interval:    time.Second,
			timeout:     time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "client",
		},
		{
			name:        "nil tracker",
			client:      client,
			interval:    time.Second,
			timeout:     time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "tracker",
		},
		{
			name:        "zero interval",
			client:      client,
			tracker:     tracker,
			timeout:     time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "interval",
		},
		{
			name:        "negative interval",
			client:      client,
			tracker:     tracker,
			interval:    -time.Second,
			timeout:     time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "interval",
		},
		{
			name:        "zero timeout",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "timeout",
		},
		{
			name:        "negative timeout",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			timeout:     -time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "timeout",
		},
		{
			name:        "empty health path",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			timeout:     time.Second,
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "health path",
		},
		{
			name:        "relative health path",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			timeout:     time.Second,
			healthPath:  "healthz",
			maxProbes:   1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "start with",
		},
		{
			name:        "zero max concurrent probes",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			timeout:     time.Second,
			healthPath:  "/healthz",
			observer:    noopHealthCheckObserver{},
			wantMessage: "max concurrent probes",
		},
		{
			name:        "negative max concurrent probes",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			timeout:     time.Second,
			healthPath:  "/healthz",
			maxProbes:   -1,
			observer:    noopHealthCheckObserver{},
			wantMessage: "max concurrent probes",
		},
		{
			name:        "nil observer",
			client:      client,
			tracker:     tracker,
			interval:    time.Second,
			timeout:     time.Second,
			healthPath:  "/healthz",
			maxProbes:   1,
			wantMessage: "observer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checker, err := NewHealthChecker(
				tt.client,
				tt.tracker,
				tt.interval,
				tt.timeout,
				tt.healthPath,
				tt.maxProbes,
				tt.observer,
			)
			if err == nil {
				t.Fatalf("NewHealthChecker() error = nil, want error containing %q", tt.wantMessage)
			}
			if checker != nil {
				t.Errorf("NewHealthChecker() checker = %#v, want nil", checker)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("NewHealthChecker() error = %q, want it to contain %q", err, tt.wantMessage)
			}
		})
	}
}

func TestHealthCheckerProbeBuildsRequestAndLimitsResponseBody(t *testing.T) {
	t.Parallel()

	body := &trackingReadCloser{
		reader: strings.NewReader(strings.Repeat("x", int(probeResultBodyLimit*2))),
	}
	var gotRequest *http.Request
	client := &http.Client{
		Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			gotRequest = request
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       body,
			}, nil
		}),
	}
	checker := newTestHealthChecker(t, client, time.Second)
	target := url.URL{
		Scheme:   "https",
		Host:     "upstream.example:8443",
		Path:     "/api",
		RawQuery: "old=true",
		Fragment: "fragment",
	}

	if err := checker.probe(context.Background(), target); err != nil {
		t.Fatalf("probe() error = %v", err)
	}

	if gotRequest == nil {
		t.Fatal("probe request = nil, want non-nil")
	}
	if gotRequest.Method != http.MethodGet {
		t.Errorf("probe method = %q, want %q", gotRequest.Method, http.MethodGet)
	}
	if got := gotRequest.URL.String(); got != "https://upstream.example:8443/healthz" {
		t.Errorf("probe URL = %q, want %q", got, "https://upstream.example:8443/healthz")
	}
	if body.bytesRead != probeResultBodyLimit {
		t.Errorf("response body bytes read = %d, want %d", body.bytesRead, probeResultBodyLimit)
	}
	if !body.closed {
		t.Error("response body was not closed")
	}
}

func TestHealthCheckerProbeClassifiesStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		statusCode int
		wantError  bool
	}{
		{statusCode: http.StatusEarlyHints, wantError: true},
		{statusCode: http.StatusOK},
		{statusCode: http.StatusNoContent},
		{statusCode: 299},
		{statusCode: http.StatusMultipleChoices, wantError: true},
		{statusCode: http.StatusServiceUnavailable, wantError: true},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.statusCode), func(t *testing.T) {
			t.Parallel()

			client := &http.Client{
				Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: tt.statusCode,
						Body:       io.NopCloser(strings.NewReader("ok")),
					}, nil
				}),
			}
			checker := newTestHealthChecker(t, client, time.Second)

			err := checker.probe(
				context.Background(),
				url.URL{Scheme: "http", Host: "upstream.example"},
			)
			if tt.wantError && err == nil {
				t.Fatalf("probe() error = nil, want status error for %d", tt.statusCode)
			}
			if !tt.wantError && err != nil {
				t.Fatalf("probe() error = %v, want nil", err)
			}
			if tt.wantError && !strings.Contains(err.Error(), strconv.Itoa(tt.statusCode)) {
				t.Errorf("probe() error = %q, want status %d", err, tt.statusCode)
			}
		})
	}
}

func TestHealthCheckerProbeWrapsTransportError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("transport failed")
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	}
	checker := newTestHealthChecker(t, client, time.Second)

	err := checker.probe(
		context.Background(),
		url.URL{Scheme: "http", Host: "upstream.example"},
	)
	if !errors.Is(err, wantErr) {
		t.Errorf("probe() error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestHealthCheckerProbeWrapsBodyReadErrorAndClosesBody(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("body read failed")
	body := &errorReadCloser{err: wantErr}
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}),
	}
	checker := newTestHealthChecker(t, client, time.Second)

	err := checker.probe(
		context.Background(),
		url.URL{Scheme: "http", Host: "upstream.example"},
	)
	if !errors.Is(err, wantErr) {
		t.Errorf("probe() error = %v, want it to wrap %v", err, wantErr)
	}
	if !body.closed {
		t.Error("response body was not closed after read error")
	}
}

func TestHealthCheckerProbeHonorsTimeout(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}
	checker := newTestHealthChecker(t, client, 10*time.Millisecond)

	err := checker.probe(
		context.Background(),
		url.URL{Scheme: "http", Host: "upstream.example"},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("probe() error = %v, want deadline exceeded", err)
	}
}

func TestHealthCheckerCheckTargetRecordsHealthTransitions(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 2, 2)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	statusCode := http.StatusServiceUnavailable
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader("health")),
			}, nil
		}),
	}
	checker, err := NewHealthChecker(
		client,
		tracker,
		time.Second,
		time.Second,
		"/healthz",
		1,
		noopHealthCheckObserver{},
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}

	firstFailure := checker.checkTarget(context.Background(), target)
	assertHealthCheckResult(t, firstFailure, target, false, false, true, true)
	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{target})

	secondFailure := checker.checkTarget(context.Background(), target)
	assertHealthCheckResult(t, secondFailure, target, false, true, true, true)
	if got := tracker.HealthyEndpoints(); len(got) != 0 {
		t.Errorf("healthy endpoints after failure threshold = %v, want none", got)
	}

	statusCode = http.StatusNoContent
	firstSuccess := checker.checkTarget(context.Background(), target)
	assertHealthCheckResult(t, firstSuccess, target, true, false, true, false)
	if got := tracker.HealthyEndpoints(); len(got) != 0 {
		t.Errorf("healthy endpoints before success threshold = %v, want none", got)
	}

	secondSuccess := checker.checkTarget(context.Background(), target)
	assertHealthCheckResult(t, secondSuccess, target, true, true, true, false)
	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{target})
}

func TestHealthCheckerCheckTargetDoesNotRecordLifecycleCancellation(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	client := &http.Client{
		Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		}),
	}
	checker, err := NewHealthChecker(
		client,
		tracker,
		time.Second,
		time.Second,
		"/healthz",
		1,
		noopHealthCheckObserver{},
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := checker.checkTarget(ctx, target)
	if !errors.Is(result.err, context.Canceled) {
		t.Errorf("checkTarget() error = %v, want context canceled", result.err)
	}
	if result.changed || result.tracked || result.healthy {
		t.Errorf("checkTarget() result after cancellation = %+v, want no recorded outcome", result)
	}
	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{target})
}

func TestHealthCheckerCheckTargetIgnoresEndpointRemovedDuringProbe(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	replacement := url.URL{Scheme: "http", Host: "replacement.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			close(started)
			<-release
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}
	checker, err := NewHealthChecker(
		client,
		tracker,
		time.Second,
		time.Second,
		"/healthz",
		1,
		noopHealthCheckObserver{},
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}
	resultCh := make(chan healthCheckResult, 1)
	go func() {
		resultCh <- checker.checkTarget(context.Background(), target)
	}()

	<-started
	if err := tracker.Replace([]url.URL{replacement}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	close(release)
	result := <-resultCh

	assertHealthCheckResult(t, result, target, true, false, false, false)
	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{replacement})
}

func TestHealthCheckerRunRoundLimitsConcurrencyAndPreservesOrder(t *testing.T) {
	t.Parallel()

	targets := []url.URL{
		{Scheme: "http", Host: "one.example"},
		{Scheme: "http", Host: "two.example"},
		{Scheme: "http", Host: "three.example"},
		{Scheme: "http", Host: "four.example"},
		{Scheme: "http", Host: "five.example"},
	}
	tracker, err := NewHealthTracker(targets, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	started := make(chan string, len(targets))
	releaseByHost := make(map[string]chan struct{}, len(targets))
	for _, target := range targets {
		releaseByHost[target.Host] = make(chan struct{})
	}
	var active atomic.Int64
	var maximum atomic.Int64
	client := &http.Client{
		Transport: healthRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			current := active.Add(1)
			for {
				previous := maximum.Load()
				if current <= previous || maximum.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- request.URL.Host
			<-releaseByHost[request.URL.Host]
			active.Add(-1)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}
	observer := &recordingHealthCheckObserver{}
	checker, err := NewHealthChecker(
		client,
		tracker,
		time.Second,
		time.Second,
		"/healthz",
		2,
		observer,
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		checker.runRound(context.Background())
		close(done)
	}()

	firstStarted := receiveStartedHost(t, started)
	secondStarted := receiveStartedHost(t, started)
	close(releaseByHost[secondStarted])
	thirdStarted := receiveStartedHost(t, started)
	close(releaseByHost[thirdStarted])
	fourthStarted := receiveStartedHost(t, started)
	close(releaseByHost[fourthStarted])
	fifthStarted := receiveStartedHost(t, started)
	close(releaseByHost[fifthStarted])
	close(releaseByHost[firstStarted])

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runRound() did not finish")
	}
	if got := maximum.Load(); got != 2 {
		t.Errorf("maximum concurrent probes = %d, want 2", got)
	}
	if got := active.Load(); got != 0 {
		t.Errorf("active probes after runRound() = %d, want 0", got)
	}
	if len(observer.probes) != len(targets) {
		t.Fatalf("observed probe count = %d, want %d", len(observer.probes), len(targets))
	}
	for i, target := range targets {
		probe := observer.probes[i]
		if probe.target.String() != target.String() {
			t.Errorf("observed probe[%d] target = %q, want %q", i, probe.target.String(), target.String())
		}
		if !probe.successful || probe.err != nil {
			t.Errorf("observed probe[%d] = %+v, want successful result", i, probe)
		}
	}
	if len(observer.transitions) != 0 {
		t.Errorf("observed transitions = %v, want none", observer.transitions)
	}
}

func TestHealthCheckerRunRoundObservesProbeResultsAndTransitions(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	statusCode := http.StatusServiceUnavailable
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader("health")),
			}, nil
		}),
	}
	observer := &recordingHealthCheckObserver{}
	checker, err := NewHealthChecker(
		client,
		tracker,
		time.Second,
		time.Second,
		"/healthz",
		1,
		observer,
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}

	checker.runRound(context.Background())
	statusCode = http.StatusNoContent
	checker.runRound(context.Background())

	if len(observer.probes) != 2 {
		t.Fatalf("observed probe count = %d, want 2", len(observer.probes))
	}
	if observer.probes[0].successful || observer.probes[0].err == nil {
		t.Errorf("first observed probe = %+v, want failed result", observer.probes[0])
	}
	if !observer.probes[1].successful || observer.probes[1].err != nil {
		t.Errorf("second observed probe = %+v, want successful result", observer.probes[1])
	}
	if len(observer.transitions) != 2 {
		t.Fatalf("observed transition count = %d, want 2", len(observer.transitions))
	}
	if observer.transitions[0].healthy {
		t.Error("first transition is healthy, want unhealthy")
	}
	if !observer.transitions[1].healthy {
		t.Error("second transition is unhealthy, want healthy")
	}
}

func TestHealthCheckerRunReturnsWithoutProbingCanceledContext(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}
	checker := newTestHealthChecker(t, client, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker.Run(ctx)

	if got := calls.Load(); got != 0 {
		t.Errorf("probe calls = %d, want 0", got)
	}
}

func TestHealthCheckerRunStartsImmediatelyAndStopsOnCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			started <- struct{}{}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}
	checker := newTestHealthChecker(t, client, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		checker.Run(ctx)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("initial health check did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}

func TestHealthCheckerRunWaitsIntervalAfterSlowRound(t *testing.T) {
	t.Parallel()

	const interval = 30 * time.Millisecond
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstFinished := make(chan time.Time, 1)
	secondStarted := make(chan time.Time, 1)
	var calls atomic.Int64
	client := &http.Client{
		Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			switch calls.Add(1) {
			case 1:
				close(firstStarted)
				<-releaseFirst
				firstFinished <- time.Now()
			case 2:
				secondStarted <- time.Now()
			}
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}
	tracker := newTestHealthTracker(t)
	checker, err := NewHealthChecker(
		client,
		tracker,
		interval,
		time.Second,
		"/healthz",
		1,
		noopHealthCheckObserver{},
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		checker.Run(ctx)
		close(done)
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first health check did not start")
	}
	time.Sleep(2 * interval)
	if got := calls.Load(); got != 1 {
		t.Fatalf("probe calls during blocked round = %d, want 1", got)
	}
	close(releaseFirst)
	finishedAt := <-firstFinished

	var startedAt time.Time
	select {
	case startedAt = <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second health check did not start")
	}
	if delay := startedAt.Sub(finishedAt); delay < interval-5*time.Millisecond {
		t.Errorf("delay between rounds = %s, want at least approximately %s", delay, interval)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
}

func newTestHealthTracker(t *testing.T) *HealthTracker {
	t.Helper()

	tracker, err := NewHealthTracker(
		[]url.URL{{Scheme: "http", Host: "upstream.example"}},
		1,
		1,
	)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	return tracker
}

func newTestHealthChecker(
	t *testing.T,
	client *http.Client,
	timeout time.Duration,
) *HealthChecker {
	t.Helper()

	checker, err := NewHealthChecker(
		client,
		newTestHealthTracker(t),
		time.Second,
		timeout,
		"/healthz",
		1,
		noopHealthCheckObserver{},
	)
	if err != nil {
		t.Fatalf("NewHealthChecker() error = %v", err)
	}
	return checker
}

func assertHealthCheckResult(
	t *testing.T,
	got healthCheckResult,
	wantTarget url.URL,
	wantHealthy bool,
	wantChanged bool,
	wantTracked bool,
	wantError bool,
) {
	t.Helper()

	if got.target.String() != wantTarget.String() {
		t.Errorf("result target = %q, want %q", got.target.String(), wantTarget.String())
	}
	if got.healthy != wantHealthy {
		t.Errorf("result healthy = %t, want %t", got.healthy, wantHealthy)
	}
	if got.changed != wantChanged {
		t.Errorf("result changed = %t, want %t", got.changed, wantChanged)
	}
	if got.tracked != wantTracked {
		t.Errorf("result tracked = %t, want %t", got.tracked, wantTracked)
	}
	if (got.err != nil) != wantError {
		t.Errorf("result error = %v, wantError %t", got.err, wantError)
	}
}

func receiveStartedHost(t *testing.T, started <-chan string) string {
	t.Helper()

	select {
	case host := <-started:
		return host
	case <-time.After(time.Second):
		t.Fatal("health probe did not start")
		return ""
	}
}

type noopHealthCheckObserver struct{}

func (noopHealthCheckObserver) RecordProbeResult(url.URL, bool, error) {}

func (noopHealthCheckObserver) RecordStateTransition(url.URL, bool) {}

type recordedHealthCheckProbe struct {
	target     url.URL
	successful bool
	err        error
}

type recordedHealthTransition struct {
	target  url.URL
	healthy bool
}

type recordingHealthCheckObserver struct {
	probes      []recordedHealthCheckProbe
	transitions []recordedHealthTransition
}

func (o *recordingHealthCheckObserver) RecordProbeResult(
	target url.URL,
	successful bool,
	err error,
) {
	o.probes = append(o.probes, recordedHealthCheckProbe{
		target:     target,
		successful: successful,
		err:        err,
	})
}

func (o *recordingHealthCheckObserver) RecordStateTransition(
	target url.URL,
	healthy bool,
) {
	o.transitions = append(o.transitions, recordedHealthTransition{
		target:  target,
		healthy: healthy,
	})
}

type healthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f healthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type trackingReadCloser struct {
	reader    io.Reader
	bytesRead int64
	closed    bool
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type errorReadCloser struct {
	err    error
	closed bool
}

func (r *errorReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *errorReadCloser) Close() error {
	r.closed = true
	return nil
}
