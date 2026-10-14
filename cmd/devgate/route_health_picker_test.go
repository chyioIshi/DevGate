package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/proxytarget"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/chyioishi/devgate/internal/upstream"
)

func TestNewRouteActiveHealthTrackerRejectsMissingUpstream(t *testing.T) {
	t.Parallel()

	tracker, err := newRouteActiveHealthTracker(nil)

	if err == nil {
		t.Fatal("newRouteActiveHealthTracker() error = nil, want missing upstream error")
	}
	if tracker != nil {
		t.Errorf("newRouteActiveHealthTracker() tracker = %#v, want nil", tracker)
	}
	if !strings.Contains(err.Error(), "route upstream was not provided") {
		t.Errorf("newRouteActiveHealthTracker() error = %q, want missing upstream context", err)
	}
}

func TestNewRouteActiveHealthTrackerReturnsNilWhenDisabled(t *testing.T) {
	t.Parallel()

	tracker, err := newRouteActiveHealthTracker(&router.Upstream{})

	if err != nil {
		t.Fatalf("newRouteActiveHealthTracker() error = %v", err)
	}
	if tracker != nil {
		t.Errorf("newRouteActiveHealthTracker() tracker = %#v, want nil", tracker)
	}
}

func TestNewRouteActiveHealthTrackerCreatesTracker(t *testing.T) {
	t.Parallel()

	endpoints := []url.URL{
		{Scheme: "http", Host: "first.example"},
		{Scheme: "https", Host: "second.example"},
	}
	routeUpstream := &router.Upstream{
		Endpoints: endpoints,
		ActiveHealthCheck: &router.ActiveHealthCheckPolicy{
			HealthyThreshold:   2,
			UnhealthyThreshold: 3,
		},
	}

	tracker, err := newRouteActiveHealthTracker(routeUpstream)

	if err != nil {
		t.Fatalf("newRouteActiveHealthTracker() error = %v", err)
	}
	if tracker == nil {
		t.Fatal("newRouteActiveHealthTracker() tracker = nil, want tracker")
	}
	if got := tracker.HealthyEndpoints(); !slices.Equal(got, endpoints) {
		t.Errorf("HealthyEndpoints() = %v, want %v", got, endpoints)
	}
}

func TestNewRouteActiveHealthTrackerWrapsConstructorError(t *testing.T) {
	t.Parallel()

	routeUpstream := &router.Upstream{
		Endpoints: []url.URL{{Scheme: "http", Host: "upstream.example"}},
		ActiveHealthCheck: &router.ActiveHealthCheckPolicy{
			HealthyThreshold:   0,
			UnhealthyThreshold: 1,
		},
	}

	tracker, err := newRouteActiveHealthTracker(routeUpstream)

	if err == nil {
		t.Fatal("newRouteActiveHealthTracker() error = nil, want constructor error")
	}
	if tracker != nil {
		t.Errorf("newRouteActiveHealthTracker() tracker = %#v, want nil", tracker)
	}
	if !strings.Contains(err.Error(), "create active health tracker") {
		t.Errorf("newRouteActiveHealthTracker() error = %q, want creation context", err)
	}
}

func TestNewRoutePassiveHealthTrackerRejectsMissingUpstream(t *testing.T) {
	t.Parallel()

	tracker, err := newRoutePassiveHealthTracker(nil)

	if err == nil {
		t.Fatal("newRoutePassiveHealthTracker() error = nil, want missing upstream error")
	}
	if tracker != nil {
		t.Errorf("newRoutePassiveHealthTracker() tracker = %#v, want nil", tracker)
	}
	if !strings.Contains(err.Error(), "route upstream was not provided") {
		t.Errorf("newRoutePassiveHealthTracker() error = %q, want missing upstream context", err)
	}
}

func TestNewRoutePassiveHealthTrackerReturnsNilWhenDisabled(t *testing.T) {
	t.Parallel()

	tracker, err := newRoutePassiveHealthTracker(&router.Upstream{})

	if err != nil {
		t.Fatalf("newRoutePassiveHealthTracker() error = %v", err)
	}
	if tracker != nil {
		t.Errorf("newRoutePassiveHealthTracker() tracker = %#v, want nil", tracker)
	}
}

func TestNewRoutePassiveHealthTrackerCreatesTracker(t *testing.T) {
	t.Parallel()

	endpoints := []url.URL{
		{Scheme: "http", Host: "first.example"},
		{Scheme: "https", Host: "second.example"},
	}
	routeUpstream := &router.Upstream{
		Endpoints: endpoints,
		PassiveHealthCheck: &router.PassiveHealthCheckPolicy{
			ConsecutiveFailures: 2,
			EjectionDuration:    time.Minute,
		},
	}

	tracker, err := newRoutePassiveHealthTracker(routeUpstream)

	if err != nil {
		t.Fatalf("newRoutePassiveHealthTracker() error = %v", err)
	}
	if tracker == nil {
		t.Fatal("newRoutePassiveHealthTracker() tracker = nil, want tracker")
	}
	if got := tracker.HealthyEndpoints(); !slices.Equal(got, endpoints) {
		t.Errorf("HealthyEndpoints() = %v, want %v", got, endpoints)
	}
}

func TestNewRoutePassiveHealthTrackerWrapsConstructorError(t *testing.T) {
	t.Parallel()

	routeUpstream := &router.Upstream{
		Endpoints: []url.URL{{Scheme: "http", Host: "upstream.example"}},
		PassiveHealthCheck: &router.PassiveHealthCheckPolicy{
			ConsecutiveFailures: 0,
			EjectionDuration:    time.Minute,
		},
	}

	tracker, err := newRoutePassiveHealthTracker(routeUpstream)

	if err == nil {
		t.Fatal("newRoutePassiveHealthTracker() error = nil, want constructor error")
	}
	if tracker != nil {
		t.Errorf("newRoutePassiveHealthTracker() tracker = %#v, want nil", tracker)
	}
	if !strings.Contains(err.Error(), "create passive health tracker") {
		t.Errorf("newRoutePassiveHealthTracker() error = %q, want creation context", err)
	}
}

func TestNewRouteHealthAwarePickerRejectsMissingTrackers(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	base, err := upstream.NewRoundRobin([]url.URL{target})
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}

	got, err := newRouteHealthAwarePicker(base, nil, nil)

	if err == nil {
		t.Fatal("newRouteHealthAwarePicker() error = nil, want missing trackers error")
	}
	if got != nil {
		t.Errorf("newRouteHealthAwarePicker() picker = %#v, want nil", got)
	}
	if !strings.Contains(err.Error(), "both active and passive health trackers cannot be nil") {
		t.Errorf("newRouteHealthAwarePicker() error = %q, want missing trackers context", err)
	}
}

func TestNewRouteHealthAwarePickerSupportsIndividualTrackers(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	activeTracker, err := upstream.NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	passiveTracker, err := upstream.NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	tests := []struct {
		name    string
		active  *upstream.HealthTracker
		passive *upstream.PassiveHealthTracker
	}{
		{name: "active only", active: activeTracker},
		{name: "passive only", passive: passiveTracker},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			base, err := upstream.NewRoundRobin([]url.URL{target})
			if err != nil {
				t.Fatalf("NewRoundRobin() error = %v", err)
			}
			picker, err := newRouteHealthAwarePicker(base, test.active, test.passive)
			if err != nil {
				t.Fatalf("newRouteHealthAwarePicker() error = %v", err)
			}

			got, release, err := picker.Acquire()
			if err != nil {
				t.Fatalf("Acquire() error = %v", err)
			}
			release()
			if got != target {
				t.Errorf("Acquire() target = %v, want %v", got, target)
			}
		})
	}
}

func TestNewRouteHealthAwarePickerIntersectsActiveAndPassiveHealth(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	endpoints := []url.URL{first, second}
	activeTracker, err := upstream.NewHealthTracker(endpoints, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	activeTracker.RecordFailure(first)
	passiveTracker, err := upstream.NewPassiveHealthTracker(
		endpoints,
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	passiveTransport, err := upstream.NewPassiveHealthTransport(
		routeHealthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       http.NoBody,
			}, nil
		}),
		passiveTracker,
		noopRoutePassiveHealthObserver{},
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTransport() error = %v", err)
	}
	requestCtx := proxytarget.WithTarget(context.Background(), second)
	response, err := passiveTransport.RoundTrip(
		(&http.Request{}).WithContext(requestCtx),
	)
	if err != nil {
		t.Fatalf("passive RoundTrip() error = %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("close passive response body: %v", closeErr)
	}
	base, err := upstream.NewRoundRobin(endpoints)
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}

	picker, err := newRouteHealthAwarePicker(base, activeTracker, passiveTracker)
	if err != nil {
		t.Fatalf("newRouteHealthAwarePicker() error = %v", err)
	}
	_, release, err := picker.Acquire()
	if !errors.Is(err, upstream.ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}
}

type routeHealthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f routeHealthRoundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return f(request)
}

type noopRoutePassiveHealthObserver struct{}

func (noopRoutePassiveHealthObserver) RecordEjection(url.URL) {}
