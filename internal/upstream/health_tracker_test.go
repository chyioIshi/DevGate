package upstream

import (
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNewHealthTrackerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tests := []struct {
		name             string
		endpoints        []url.URL
		successThreshold int
		failureThreshold int
		wantMessage      string
	}{
		{
			name:             "nil endpoints",
			successThreshold: 1,
			failureThreshold: 1,
			wantMessage:      "no endpoints",
		},
		{
			name:             "empty endpoints",
			endpoints:        []url.URL{},
			successThreshold: 1,
			failureThreshold: 1,
			wantMessage:      "no endpoints",
		},
		{
			name:             "zero success threshold",
			endpoints:        []url.URL{endpoint},
			successThreshold: 0,
			failureThreshold: 1,
			wantMessage:      "success threshold",
		},
		{
			name:             "negative success threshold",
			endpoints:        []url.URL{endpoint},
			successThreshold: -1,
			failureThreshold: 1,
			wantMessage:      "success threshold",
		},
		{
			name:             "zero failure threshold",
			endpoints:        []url.URL{endpoint},
			successThreshold: 1,
			failureThreshold: 0,
			wantMessage:      "failure threshold",
		},
		{
			name:             "negative failure threshold",
			endpoints:        []url.URL{endpoint},
			successThreshold: 1,
			failureThreshold: -1,
			wantMessage:      "failure threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewHealthTracker(
				tt.endpoints,
				tt.successThreshold,
				tt.failureThreshold,
			)
			if err == nil {
				t.Fatalf("NewHealthTracker() error = nil, want error containing %q", tt.wantMessage)
			}
			if got != nil {
				t.Errorf("NewHealthTracker() tracker = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("NewHealthTracker() error = %q, want it to contain %q", err, tt.wantMessage)
			}
		})
	}
}

func TestNewHealthTrackerCopiesEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := []url.URL{{Scheme: "http", Host: "original.example"}}
	tracker, err := NewHealthTracker(endpoints, 2, 3)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	endpoints[0].Host = "mutated.example"
	if got := tracker.order[0].Host; got != "original.example" {
		t.Errorf("stored endpoint host = %q, want %q", got, "original.example")
	}
	if _, exists := tracker.healthByTarget["http://original.example"]; !exists {
		t.Error("health state for original endpoint does not exist")
	}
	if _, exists := tracker.healthByTarget["http://mutated.example"]; exists {
		t.Error("input mutation changed the tracker's health map")
	}
}

func TestNewHealthTrackerSharesHealthStateForDuplicateEndpoints(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint, endpoint}, 2, 3)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	if got := len(tracker.order); got != 2 {
		t.Errorf("order length = %d, want 2 configured entries", got)
	}
	if got := len(tracker.healthByTarget); got != 1 {
		t.Errorf("health state count = %d, want 1 shared state", got)
	}
	if health := tracker.healthByTarget[endpoint.String()]; health == nil {
		t.Error("shared health state does not exist")
	}
}

func TestNewHealthTrackerUsesURLStringAsIdentity(t *testing.T) {
	t.Parallel()

	first, err := url.Parse("http://user:password@upstream.example/api")
	if err != nil {
		t.Fatalf("parse first URL: %v", err)
	}
	second, err := url.Parse("http://user:password@upstream.example/api")
	if err != nil {
		t.Fatalf("parse second URL: %v", err)
	}

	tracker, err := NewHealthTracker([]url.URL{*first, *second}, 2, 3)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	if got := len(tracker.healthByTarget); got != 1 {
		t.Errorf("health state count = %d, want 1 for equivalent URLs", got)
	}
}

func TestNewHealthTrackerInitializesEndpointState(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "https", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint}, 2, 3)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	health := tracker.healthByTarget[endpoint.String()]
	if health == nil {
		t.Fatal("endpoint health state = nil, want non-nil")
	}
	if !health.isHealthy() {
		t.Error("new endpoint is unhealthy, want healthy")
	}
	if health.successThreshold != 2 {
		t.Errorf("success threshold = %d, want 2", health.successThreshold)
	}
	if health.failureThreshold != 3 {
		t.Errorf("failure threshold = %d, want 3", health.failureThreshold)
	}
}

func TestHealthTrackerRecordsStateTransitions(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint}, 2, 2)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	if changed, tracked := tracker.RecordFailure(endpoint); changed || !tracked {
		t.Errorf(
			"first RecordFailure() = (%t, %t), want (false, true)",
			changed,
			tracked,
		)
	}
	if changed, tracked := tracker.RecordFailure(endpoint); !changed || !tracked {
		t.Errorf(
			"second RecordFailure() = (%t, %t), want (true, true)",
			changed,
			tracked,
		)
	}

	if changed, tracked := tracker.RecordSuccess(endpoint); changed || !tracked {
		t.Errorf(
			"first RecordSuccess() = (%t, %t), want (false, true)",
			changed,
			tracked,
		)
	}
	if changed, tracked := tracker.RecordSuccess(endpoint); !changed || !tracked {
		t.Errorf(
			"second RecordSuccess() = (%t, %t), want (true, true)",
			changed,
			tracked,
		)
	}
}

func TestHealthTrackerIgnoresUnknownEndpoint(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	unknown := url.URL{Scheme: "http", Host: "unknown.example"}

	if changed, tracked := tracker.RecordSuccess(unknown); changed || tracked {
		t.Errorf(
			"RecordSuccess(unknown) = (%t, %t), want (false, false)",
			changed,
			tracked,
		)
	}
	if changed, tracked := tracker.RecordFailure(unknown); changed || tracked {
		t.Errorf(
			"RecordFailure(unknown) = (%t, %t), want (false, false)",
			changed,
			tracked,
		)
	}
}

func TestHealthTrackerRecordsFailuresConcurrently(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	const goroutines = 100
	var transitions atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if changed, _ := tracker.RecordFailure(endpoint); changed {
				transitions.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := transitions.Load(); got != 1 {
		t.Errorf("health transitions = %d, want 1", got)
	}
}

func TestHealthTrackerHealthyEndpointsPreservesOrderAndDuplicates(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	tracker, err := NewHealthTracker([]url.URL{first, second, first}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	got := tracker.HealthyEndpoints()
	want := []url.URL{first, second, first}
	assertEndpointURLs(t, got, want)
}

func TestHealthTrackerHealthyEndpointsFiltersAllOccurrences(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	tracker, err := NewHealthTracker([]url.URL{first, second, first}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	if changed, tracked := tracker.RecordFailure(first); !changed || !tracked {
		t.Fatalf(
			"RecordFailure() = (%t, %t), want (true, true)",
			changed,
			tracked,
		)
	}

	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{second})

	if changed, tracked := tracker.RecordFailure(second); !changed || !tracked {
		t.Fatalf(
			"RecordFailure() = (%t, %t), want (true, true)",
			changed,
			tracked,
		)
	}
	if got := tracker.HealthyEndpoints(); len(got) != 0 {
		t.Errorf("HealthyEndpoints() = %v, want no endpoints", got)
	}
}

func TestHealthTrackerHealthyEndpointsReturnsIndependentSnapshot(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	snapshot := tracker.HealthyEndpoints()
	snapshot[0].Host = "mutated.example"

	got := tracker.HealthyEndpoints()
	assertEndpointURLs(t, got, []url.URL{endpoint})
}

func assertEndpointURLs(t *testing.T, got, want []url.URL) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("endpoint count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].String() != want[i].String() {
			t.Errorf(
				"endpoint[%d] = %q, want %q",
				i,
				got[i].String(),
				want[i].String(),
			)
		}
	}
}

func TestHealthTrackerReplacePreservesExistingState(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	removed := url.URL{Scheme: "http", Host: "removed.example"}
	added := url.URL{Scheme: "http", Host: "added.example"}
	tracker, err := NewHealthTracker([]url.URL{first, removed}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	if changed, tracked := tracker.RecordFailure(first); !changed || !tracked {
		t.Fatalf(
			"RecordFailure() = (%t, %t), want (true, true)",
			changed,
			tracked,
		)
	}
	originalState := tracker.healthByTarget[first.String()]

	if err := tracker.Replace([]url.URL{first, added, first}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	if got := tracker.healthByTarget[first.String()]; got != originalState {
		t.Error("Replace() did not preserve the existing endpoint health state")
	}
	if got := len(tracker.healthByTarget); got != 2 {
		t.Errorf("health state count = %d, want 2", got)
	}
	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{added})
	if changed, tracked := tracker.RecordSuccess(removed); changed || tracked {
		t.Errorf(
			"RecordSuccess(removed) = (%t, %t), want (false, false)",
			changed,
			tracked,
		)
	}
}

func TestHealthTrackerReplaceStartsNewEndpointHealthy(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	added := url.URL{Scheme: "http", Host: "added.example"}
	tracker, err := NewHealthTracker([]url.URL{first}, 2, 3)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	if err := tracker.Replace([]url.URL{added}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{added})
	health := tracker.healthByTarget[added.String()]
	if health == nil {
		t.Fatal("new endpoint health state = nil, want non-nil")
	}
	if health.target.String() != added.String() {
		t.Errorf("stored target = %q, want %q", health.target.String(), added.String())
	}
	if health.successThreshold != 2 || health.failureThreshold != 3 {
		t.Errorf(
			"new endpoint thresholds = (%d, %d), want (2, 3)",
			health.successThreshold,
			health.failureThreshold,
		)
	}
}

func TestHealthTrackerReplaceRejectsEmptySnapshot(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{endpoint}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	originalState := tracker.healthByTarget[endpoint.String()]

	if err := tracker.Replace(nil); err == nil {
		t.Fatal("Replace(nil) error = nil, want an error")
	}

	assertEndpointURLs(t, tracker.HealthyEndpoints(), []url.URL{endpoint})
	if got := tracker.healthByTarget[endpoint.String()]; got != originalState {
		t.Error("failed Replace() changed the endpoint health state")
	}
}

func TestHealthTrackerReplaceCopiesEndpoints(t *testing.T) {
	t.Parallel()

	initial := url.URL{Scheme: "http", Host: "initial.example"}
	replacement := []url.URL{{Scheme: "http", Host: "replacement.example"}}
	tracker, err := NewHealthTracker([]url.URL{initial}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	if err := tracker.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	replacement[0].Host = "mutated.example"

	want := []url.URL{{Scheme: "http", Host: "replacement.example"}}
	assertEndpointURLs(t, tracker.HealthyEndpoints(), want)
}

func TestHealthTrackerReplaceIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	tracker, err := NewHealthTracker([]url.URL{first, second}, 2, 2)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for range 100 {
			if err := tracker.Replace([]url.URL{first, second}); err != nil {
				t.Errorf("Replace() error = %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			tracker.RecordFailure(first)
			tracker.RecordSuccess(first)
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			tracker.HealthyEndpoints()
		}
	}()
	wg.Wait()
}
