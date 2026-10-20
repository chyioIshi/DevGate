package upstream

import (
	"errors"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestNewEndpointCoordinatorValidatesRequiredDependencies(t *testing.T) {
	t.Parallel()

	membership := NewEndpointMembership(nil)
	tests := []struct {
		name       string
		membership *EndpointMembership
		wantError  string
	}{
		{
			name:      "missing membership",
			wantError: "endpoint membership was not provided",
		},
		{
			name:       "missing picker",
			membership: membership,
			wantError:  "health-aware picker was not provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			coordinator, err := NewEndpointCoordinator(
				test.membership,
				nil,
				nil,
				nil,
			)

			if err == nil {
				t.Fatal("NewEndpointCoordinator() error = nil, want error")
			}
			if coordinator != nil {
				t.Errorf("NewEndpointCoordinator() coordinator = %#v, want nil", coordinator)
			}
			if err.Error() != test.wantError {
				t.Errorf(
					"NewEndpointCoordinator() error = %q, want %q",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestEndpointCoordinatorApplySupportsOptionalHealthTrackers(t *testing.T) {
	t.Parallel()

	initial := url.URL{Scheme: "http", Host: "initial.example"}
	replacement := url.URL{Scheme: "http", Host: "replacement.example"}
	tests := []struct {
		name           string
		includeActive  bool
		includePassive bool
	}{
		{name: "membership only"},
		{name: "active only", includeActive: true},
		{name: "passive only", includePassive: true},
		{name: "active and passive", includeActive: true, includePassive: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			coordinator, picker, membership, active, passive := newEndpointCoordinatorTestFixture(
				t,
				[]url.URL{initial},
				test.includeActive,
				test.includePassive,
			)

			if err := coordinator.Apply([]url.URL{replacement}); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			if got := membership.HealthyEndpoints(); !slices.Equal(got, []url.URL{replacement}) {
				t.Errorf("membership endpoints = %v, want %v", got, []url.URL{replacement})
			}
			if active != nil {
				if got := active.Targets(); !slices.Equal(got, []url.URL{replacement}) {
					t.Errorf("active targets = %v, want %v", got, []url.URL{replacement})
				}
			}
			if passive != nil {
				if got := passive.HealthyEndpoints(); !slices.Equal(got, []url.URL{replacement}) {
					t.Errorf("passive endpoints = %v, want %v", got, []url.URL{replacement})
				}
			}
			got, release, err := picker.Acquire()
			if err != nil {
				t.Fatalf("Acquire() error = %v", err)
			}
			release()
			if got != replacement {
				t.Errorf("Acquire() target = %v, want %v", got, replacement)
			}
		})
	}
}

func TestEndpointCoordinatorApplyPreservesRetainedHealthState(t *testing.T) {
	t.Parallel()

	retained := url.URL{Scheme: "http", Host: "retained.example"}
	removed := url.URL{Scheme: "http", Host: "removed.example"}
	added := url.URL{Scheme: "http", Host: "added.example"}
	coordinator, picker, _, active, passive := newEndpointCoordinatorTestFixture(
		t,
		[]url.URL{retained, removed},
		true,
		true,
	)
	active.RecordFailure(retained)
	permit, ok := passive.acquirePermit(retained)
	if !ok {
		t.Fatal("acquirePermit() allowed = false, want true")
	}
	if !passive.recordOutcome(permit, passiveHealthOutcomeFailure) {
		t.Fatal("recordOutcome() ejected = false, want true")
	}

	if err := coordinator.Apply([]url.URL{retained, added}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if got := active.HealthyEndpoints(); !slices.Equal(got, []url.URL{added}) {
		t.Errorf("active healthy endpoints = %v, want %v", got, []url.URL{added})
	}
	if got := passive.HealthyEndpoints(); !slices.Equal(got, []url.URL{added}) {
		t.Errorf("passive healthy endpoints = %v, want %v", got, []url.URL{added})
	}
	got, release, err := picker.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	release()
	if got != added {
		t.Errorf("Acquire() target = %v, want %v", got, added)
	}
}

func TestEndpointCoordinatorApplyEmptySnapshotAndRestore(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	coordinator, picker, membership, active, passive := newEndpointCoordinatorTestFixture(
		t,
		[]url.URL{target},
		true,
		true,
	)
	stalePermit, ok := passive.acquirePermit(target)
	if !ok {
		t.Fatal("acquirePermit() allowed = false, want true")
	}

	if err := coordinator.Apply(nil); err != nil {
		t.Fatalf("Apply(nil) error = %v", err)
	}
	if len(membership.HealthyEndpoints()) != 0 {
		t.Errorf("membership after Apply(nil) = %v, want empty", membership.HealthyEndpoints())
	}
	if len(active.Targets()) != 0 {
		t.Errorf("active targets after Apply(nil) = %v, want empty", active.Targets())
	}
	if len(passive.HealthyEndpoints()) != 0 {
		t.Errorf("passive endpoints after Apply(nil) = %v, want empty", passive.HealthyEndpoints())
	}
	_, release, err := picker.Acquire()
	if !errors.Is(err, ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}

	if err := coordinator.Apply([]url.URL{target}); err != nil {
		t.Fatalf("Apply() restoring endpoint error = %v", err)
	}
	if passive.recordOutcome(stalePermit, passiveHealthOutcomeFailure) {
		t.Error("stale permit ejected restored endpoint")
	}
	got, release, err := picker.Acquire()
	if err != nil {
		t.Fatalf("Acquire() after restore error = %v", err)
	}
	release()
	if got != target {
		t.Errorf("Acquire() after restore target = %v, want %v", got, target)
	}
}

func TestEndpointCoordinatorApplyIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	coordinator, picker, _, _, _ := newEndpointCoordinatorTestFixture(
		t,
		[]url.URL{first},
		true,
		true,
	)
	snapshots := [][]url.URL{{first}, nil, {second}, {first, second}}

	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for iteration := range 100 {
				if err := coordinator.Apply(snapshots[(worker+iteration)%len(snapshots)]); err != nil {
					t.Errorf("Apply() error = %v", err)
					return
				}
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range 100 {
				_, release, err := picker.Acquire()
				if err != nil {
					if !errors.Is(err, ErrNoHealthyEndpoints) {
						t.Errorf("Acquire() error = %v", err)
						return
					}
					continue
				}
				release()
			}
		}()
	}
	wg.Wait()
}

func newEndpointCoordinatorTestFixture(
	t *testing.T,
	endpoints []url.URL,
	includeActive bool,
	includePassive bool,
) (
	*EndpointCoordinator,
	*HealthAwarePicker,
	*EndpointMembership,
	*HealthTracker,
	*PassiveHealthTracker,
) {
	t.Helper()

	membership := NewEndpointMembership(endpoints)
	sources := []healthyEndpointSource{membership}
	var active *HealthTracker
	if includeActive {
		var err error
		active, err = NewHealthTracker(endpoints, 1, 1)
		if err != nil {
			t.Fatalf("NewHealthTracker() error = %v", err)
		}
		sources = append(sources, active)
	}
	var passive *PassiveHealthTracker
	if includePassive {
		var err error
		passive, err = NewPassiveHealthTracker(endpoints, 1, time.Hour)
		if err != nil {
			t.Fatalf("NewPassiveHealthTracker() error = %v", err)
		}
		sources = append(sources, passive)
	}
	composite, err := NewCompositeHealthSource(sources...)
	if err != nil {
		t.Fatalf("NewCompositeHealthSource() error = %v", err)
	}
	base, err := NewRoundRobin(endpoints)
	if err != nil {
		t.Fatalf("NewRoundRobin() error = %v", err)
	}
	picker, err := NewHealthAwarePicker(base, composite)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	coordinator, err := NewEndpointCoordinator(membership, picker, active, passive)
	if err != nil {
		t.Fatalf("NewEndpointCoordinator() error = %v", err)
	}
	return coordinator, picker, membership, active, passive
}
