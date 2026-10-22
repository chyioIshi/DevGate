package discovery

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewDNSWatcherValidatesDependenciesAndTiming(t *testing.T) {
	t.Parallel()

	resolver := endpointResolverFunc(func(
		context.Context,
		url.URL,
	) ([]url.URL, error) {
		return nil, nil
	})
	applier := endpointSnapshotApplierFunc(func([]url.URL) error {
		return nil
	})
	observer := dnsRefreshObserverFunc(func(url.URL, int, error) {})
	tests := []struct {
		name            string
		resolver        EndpointResolver
		applier         EndpointSnapshotApplier
		observer        DNSRefreshObserver
		refreshInterval time.Duration
		lookupTimeout   time.Duration
		wantError       string
	}{
		{
			name:          "missing endpoint resolver",
			wantError:     "endpoint resolver was not provided",
			lookupTimeout: time.Second,
		},
		{
			name:          "missing endpoint snapshot applier",
			resolver:      resolver,
			wantError:     "endpoint snapshot applier was not provided",
			lookupTimeout: time.Second,
		},
		{
			name:          "missing DNS refresh observer",
			resolver:      resolver,
			applier:       applier,
			wantError:     "DNS refresh observer was not provided",
			lookupTimeout: time.Second,
		},
		{
			name:          "zero refresh interval",
			resolver:      resolver,
			applier:       applier,
			observer:      observer,
			wantError:     "refresh interval must be greater than 0",
			lookupTimeout: time.Second,
		},
		{
			name:            "negative refresh interval",
			resolver:        resolver,
			applier:         applier,
			observer:        observer,
			refreshInterval: -time.Second,
			lookupTimeout:   time.Second,
			wantError:       "refresh interval must be greater than 0",
		},
		{
			name:            "zero lookup timeout",
			resolver:        resolver,
			applier:         applier,
			observer:        observer,
			refreshInterval: time.Second,
			wantError:       "lookup timeout must be greater than 0",
		},
		{
			name:            "negative lookup timeout",
			resolver:        resolver,
			applier:         applier,
			observer:        observer,
			refreshInterval: time.Second,
			lookupTimeout:   -time.Second,
			wantError:       "lookup timeout must be greater than 0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			watcher, err := NewDNSWatcher(
				test.resolver,
				test.applier,
				test.observer,
				url.URL{Scheme: "http", Host: "service.example"},
				test.refreshInterval,
				test.lookupTimeout,
			)

			if err == nil {
				t.Fatal("NewDNSWatcher() error = nil, want error")
			}
			if watcher != nil {
				t.Errorf("NewDNSWatcher() watcher = %#v, want nil", watcher)
			}
			if err.Error() != test.wantError {
				t.Errorf("NewDNSWatcher() error = %q, want %q", err, test.wantError)
			}
		})
	}
}

func TestNewDNSWatcherStoresConfiguration(t *testing.T) {
	t.Parallel()

	resolver := endpointResolverFunc(func(
		context.Context,
		url.URL,
	) ([]url.URL, error) {
		return nil, nil
	})
	applier := endpointSnapshotApplierFunc(func([]url.URL) error {
		return nil
	})
	observer := dnsRefreshObserverFunc(func(url.URL, int, error) {})
	target := url.URL{Scheme: "http", Host: "service.example:8080", Path: "/api"}
	refreshInterval := 30 * time.Second
	lookupTimeout := 2 * time.Second

	watcher, err := NewDNSWatcher(
		resolver,
		applier,
		observer,
		target,
		refreshInterval,
		lookupTimeout,
	)

	if err != nil {
		t.Fatalf("NewDNSWatcher() error = %v", err)
	}
	if watcher.resolver == nil {
		t.Error("NewDNSWatcher() did not store endpoint resolver")
	}
	if watcher.applier == nil {
		t.Error("NewDNSWatcher() did not store endpoint snapshot applier")
	}
	if watcher.observer == nil {
		t.Error("NewDNSWatcher() did not store DNS refresh observer")
	}
	if watcher.target != target {
		t.Errorf("NewDNSWatcher() target = %v, want %v", watcher.target, target)
	}
	if watcher.refreshInterval != refreshInterval {
		t.Errorf(
			"NewDNSWatcher() refresh interval = %v, want %v",
			watcher.refreshInterval,
			refreshInterval,
		)
	}
	if watcher.lookupTimeout != lookupTimeout {
		t.Errorf(
			"NewDNSWatcher() lookup timeout = %v, want %v",
			watcher.lookupTimeout,
			lookupTimeout,
		)
	}
}

func TestDNSWatcherRefreshAppliesSuccessfulResolution(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "service.example"}
	first := url.URL{Scheme: "http", Host: "192.0.2.10:80"}
	second := url.URL{Scheme: "http", Host: "[2001:db8::10]:80"}
	tests := []struct {
		name      string
		endpoints []url.URL
	}{
		{name: "non-empty snapshot", endpoints: []url.URL{first, second}},
		{name: "empty snapshot"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var applied []url.URL
			var observerCalls int
			var observedCount int
			var observedErr error
			watcher := mustNewDNSWatcher(
				t,
				endpointResolverFunc(func(
					ctx context.Context,
					gotTarget url.URL,
				) ([]url.URL, error) {
					if gotTarget != target {
						t.Errorf("Resolve() target = %v, want %v", gotTarget, target)
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Error("Resolve() context has no lookup deadline")
					}
					return test.endpoints, nil
				}),
				endpointSnapshotApplierFunc(func(endpoints []url.URL) error {
					applied = slices.Clone(endpoints)
					return nil
				}),
				dnsRefreshObserverFunc(func(
					gotTarget url.URL,
					endpointCount int,
					err error,
				) {
					observerCalls++
					if gotTarget != target {
						t.Errorf("RecordRefresh() target = %v, want %v", gotTarget, target)
					}
					observedCount = endpointCount
					observedErr = err
				}),
				target,
			)

			watcher.refresh(context.Background())

			if !slices.Equal(applied, test.endpoints) {
				t.Errorf("Apply() endpoints = %v, want %v", applied, test.endpoints)
			}
			if observerCalls != 1 {
				t.Errorf("RecordRefresh() calls = %d, want 1", observerCalls)
			}
			if observedCount != len(test.endpoints) {
				t.Errorf(
					"RecordRefresh() endpoint count = %d, want %d",
					observedCount,
					len(test.endpoints),
				)
			}
			if observedErr != nil {
				t.Errorf("RecordRefresh() error = %v, want nil", observedErr)
			}
		})
	}
}

func TestDNSWatcherRefreshKeepsSnapshotOnResolveError(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "service.example"}
	wantErr := errors.New("DNS unavailable")
	applyCalled := false
	var observerCalls int
	var observedCount int
	var observedErr error
	watcher := mustNewDNSWatcher(
		t,
		endpointResolverFunc(func(
			context.Context,
			url.URL,
		) ([]url.URL, error) {
			return nil, wantErr
		}),
		endpointSnapshotApplierFunc(func([]url.URL) error {
			applyCalled = true
			return nil
		}),
		dnsRefreshObserverFunc(func(_ url.URL, endpointCount int, err error) {
			observerCalls++
			observedCount = endpointCount
			observedErr = err
		}),
		target,
	)

	watcher.refresh(context.Background())

	if applyCalled {
		t.Error("Apply() was called after a resolution error")
	}
	if observerCalls != 1 {
		t.Errorf("RecordRefresh() calls = %d, want 1", observerCalls)
	}
	if observedCount != 0 {
		t.Errorf("RecordRefresh() endpoint count = %d, want 0", observedCount)
	}
	if !errors.Is(observedErr, wantErr) {
		t.Errorf("RecordRefresh() error = %v, want wrapped resolution error", observedErr)
	}
	if !strings.Contains(observedErr.Error(), "resolve DNS endpoints") {
		t.Errorf("RecordRefresh() error = %q, want resolution context", observedErr)
	}
}

func TestDNSWatcherRefreshReportsApplyError(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "service.example"}
	endpoints := []url.URL{
		{Scheme: "http", Host: "192.0.2.10:80"},
		{Scheme: "http", Host: "192.0.2.11:80"},
	}
	wantErr := errors.New("publication failed")
	var observerCalls int
	var observedCount int
	var observedErr error
	watcher := mustNewDNSWatcher(
		t,
		endpointResolverFunc(func(
			context.Context,
			url.URL,
		) ([]url.URL, error) {
			return endpoints, nil
		}),
		endpointSnapshotApplierFunc(func(got []url.URL) error {
			if !slices.Equal(got, endpoints) {
				t.Errorf("Apply() endpoints = %v, want %v", got, endpoints)
			}
			return wantErr
		}),
		dnsRefreshObserverFunc(func(_ url.URL, endpointCount int, err error) {
			observerCalls++
			observedCount = endpointCount
			observedErr = err
		}),
		target,
	)

	watcher.refresh(context.Background())

	if observerCalls != 1 {
		t.Errorf("RecordRefresh() calls = %d, want 1", observerCalls)
	}
	if observedCount != len(endpoints) {
		t.Errorf("RecordRefresh() endpoint count = %d, want %d", observedCount, len(endpoints))
	}
	if !errors.Is(observedErr, wantErr) {
		t.Errorf("RecordRefresh() error = %v, want wrapped apply error", observedErr)
	}
	if !strings.Contains(observedErr.Error(), "apply DNS endpoint snapshot") {
		t.Errorf("RecordRefresh() error = %q, want apply context", observedErr)
	}
}

func TestDNSWatcherRefreshEnforcesLookupTimeout(t *testing.T) {
	t.Parallel()

	applyCalled := false
	var observedErr error
	watcher, err := NewDNSWatcher(
		endpointResolverFunc(func(
			ctx context.Context,
			_ url.URL,
		) ([]url.URL, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
		endpointSnapshotApplierFunc(func([]url.URL) error {
			applyCalled = true
			return nil
		}),
		dnsRefreshObserverFunc(func(_ url.URL, _ int, err error) {
			observedErr = err
		}),
		url.URL{Scheme: "http", Host: "service.example"},
		time.Minute,
		10*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("NewDNSWatcher() error = %v", err)
	}

	watcher.refresh(context.Background())

	if applyCalled {
		t.Error("Apply() was called after lookup timeout")
	}
	if !errors.Is(observedErr, context.DeadlineExceeded) {
		t.Errorf("RecordRefresh() error = %v, want context.DeadlineExceeded", observedErr)
	}
}

func TestDNSWatcherRefreshStopsQuietlyWhenParentContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	applyCalled := false
	observerCalled := false
	watcher := mustNewDNSWatcher(
		t,
		endpointResolverFunc(func(
			context.Context,
			url.URL,
		) ([]url.URL, error) {
			cancel()
			return []url.URL{{Scheme: "http", Host: "192.0.2.10:80"}}, nil
		}),
		endpointSnapshotApplierFunc(func([]url.URL) error {
			applyCalled = true
			return nil
		}),
		dnsRefreshObserverFunc(func(url.URL, int, error) {
			observerCalled = true
		}),
		url.URL{Scheme: "http", Host: "service.example"},
	)

	watcher.refresh(ctx)

	if applyCalled {
		t.Error("Apply() was called after parent context cancellation")
	}
	if observerCalled {
		t.Error("RecordRefresh() was called for lifecycle cancellation")
	}
}

func TestDNSWatcherRunRefreshesImmediatelyThenAfterFixedDelay(t *testing.T) {
	t.Parallel()

	const refreshInterval = 200 * time.Millisecond
	resolveCalls := make(chan struct{}, 3)
	watcher, err := NewDNSWatcher(
		endpointResolverFunc(func(
			context.Context,
			url.URL,
		) ([]url.URL, error) {
			resolveCalls <- struct{}{}
			return []url.URL{{Scheme: "http", Host: "192.0.2.10:80"}}, nil
		}),
		endpointSnapshotApplierFunc(func([]url.URL) error {
			return nil
		}),
		dnsRefreshObserverFunc(func(url.URL, int, error) {}),
		url.URL{Scheme: "http", Host: "service.example"},
		refreshInterval,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewDNSWatcher() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case <-resolveCalls:
	case <-time.After(time.Second):
		t.Fatal("initial Resolve() was not called")
	}
	select {
	case <-resolveCalls:
		t.Fatal("second Resolve() was called without waiting for refresh interval")
	case <-time.After(refreshInterval / 4):
	}
	select {
	case <-resolveCalls:
	case <-time.After(time.Second):
		t.Fatal("periodic Resolve() was not called")
	}
}

func TestDNSWatcherRunStopsQuietlyDuringLookup(t *testing.T) {
	t.Parallel()

	lookupStarted := make(chan struct{})
	var applyCalled atomic.Bool
	var observerCalled atomic.Bool
	watcher := mustNewDNSWatcher(
		t,
		endpointResolverFunc(func(
			ctx context.Context,
			_ url.URL,
		) ([]url.URL, error) {
			close(lookupStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		}),
		endpointSnapshotApplierFunc(func([]url.URL) error {
			applyCalled.Store(true)
			return nil
		}),
		dnsRefreshObserverFunc(func(url.URL, int, error) {
			observerCalled.Store(true)
		}),
		url.URL{Scheme: "http", Host: "service.example"},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		watcher.Run(ctx)
	}()

	select {
	case <-lookupStarted:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("Resolve() was not called")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after context cancellation")
	}
	if applyCalled.Load() {
		t.Error("Apply() was called during shutdown")
	}
	if observerCalled.Load() {
		t.Error("RecordRefresh() was called during shutdown")
	}
}

func TestDNSWatcherRunReturnsWhenContextIsAlreadyCanceled(t *testing.T) {
	t.Parallel()

	var resolveCalled atomic.Bool
	watcher := mustNewDNSWatcher(
		t,
		endpointResolverFunc(func(
			context.Context,
			url.URL,
		) ([]url.URL, error) {
			resolveCalled.Store(true)
			return nil, nil
		}),
		endpointSnapshotApplierFunc(func([]url.URL) error {
			return nil
		}),
		dnsRefreshObserverFunc(func(url.URL, int, error) {}),
		url.URL{Scheme: "http", Host: "service.example"},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	watcher.Run(ctx)

	if resolveCalled.Load() {
		t.Error("Resolve() was called with an already canceled context")
	}
}

type endpointResolverFunc func(context.Context, url.URL) ([]url.URL, error)

func (f endpointResolverFunc) Resolve(
	ctx context.Context,
	target url.URL,
) ([]url.URL, error) {
	return f(ctx, target)
}

type endpointSnapshotApplierFunc func([]url.URL) error

func (f endpointSnapshotApplierFunc) Apply(endpoints []url.URL) error {
	return f(endpoints)
}

type dnsRefreshObserverFunc func(url.URL, int, error)

func (f dnsRefreshObserverFunc) RecordRefresh(
	target url.URL,
	endpointCount int,
	err error,
) {
	f(target, endpointCount, err)
}

func mustNewDNSWatcher(
	t *testing.T,
	resolver EndpointResolver,
	applier EndpointSnapshotApplier,
	observer DNSRefreshObserver,
	target url.URL,
) *DNSWatcher {
	t.Helper()

	watcher, err := NewDNSWatcher(
		resolver,
		applier,
		observer,
		target,
		time.Minute,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewDNSWatcher() error = %v", err)
	}
	return watcher
}
