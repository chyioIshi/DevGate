package upstream

import (
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNewPassiveHealthTrackerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	endpoint := url.URL{Scheme: "http", Host: "upstream.example"}
	tests := []struct {
		name             string
		endpoints        []url.URL
		failureThreshold int
		ejectionDuration time.Duration
		wantMessage      string
	}{
		{
			name:             "no endpoints",
			failureThreshold: 1,
			ejectionDuration: time.Second,
			wantMessage:      "no endpoints",
		},
		{
			name:             "zero failure threshold",
			endpoints:        []url.URL{endpoint},
			ejectionDuration: time.Second,
			wantMessage:      "failure threshold",
		},
		{
			name:             "negative failure threshold",
			endpoints:        []url.URL{endpoint},
			failureThreshold: -1,
			ejectionDuration: time.Second,
			wantMessage:      "failure threshold",
		},
		{
			name:             "zero ejection duration",
			endpoints:        []url.URL{endpoint},
			failureThreshold: 1,
			wantMessage:      "ejection duration",
		},
		{
			name:             "negative ejection duration",
			endpoints:        []url.URL{endpoint},
			failureThreshold: 1,
			ejectionDuration: -time.Second,
			wantMessage:      "ejection duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewPassiveHealthTracker(
				tt.endpoints,
				tt.failureThreshold,
				tt.ejectionDuration,
			)
			if err == nil {
				t.Fatalf("NewPassiveHealthTracker() error = nil, want %q", tt.wantMessage)
			}
			if got != nil {
				t.Errorf("NewPassiveHealthTracker() tracker = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("NewPassiveHealthTracker() error = %q, want it to contain %q", err, tt.wantMessage)
			}
		})
	}
}

func TestNewPassiveHealthTrackerInitializesIndependentState(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	endpoints := []url.URL{first, second, first}

	tracker, err := NewPassiveHealthTracker(endpoints, 3, 30*time.Second)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	endpoints[0].Host = "mutated.example"

	if got := len(tracker.ordered); got != 3 {
		t.Errorf("ordered endpoint count = %d, want 3", got)
	}
	if got := tracker.ordered[0]; got != first {
		t.Errorf("first ordered endpoint = %q, want %q", got.String(), first.String())
	}
	if got := len(tracker.stateByTarget); got != 2 {
		t.Errorf("unique endpoint state count = %d, want 2", got)
	}
	for _, target := range []url.URL{first, second} {
		state := tracker.stateByTarget[target.String()]
		if state == nil {
			t.Fatalf("state for target %q = nil", target.String())
		}
		if state.target != target {
			t.Errorf("state target = %q, want %q", state.target.String(), target.String())
		}
		if state.consecutiveFailures != 0 || !state.ejectionUntil.IsZero() || state.generation != 0 {
			t.Errorf("initial state for target %q = %+v, want zero state", target.String(), state)
		}
	}
	if tracker.failureThreshold != 3 {
		t.Errorf("failure threshold = %d, want 3", tracker.failureThreshold)
	}
	if tracker.ejectionDuration != 30*time.Second {
		t.Errorf("ejection duration = %s, want 30s", tracker.ejectionDuration)
	}
	if tracker.now == nil {
		t.Error("clock function = nil, want time.Now dependency")
	}
}

func TestPassiveHealthTrackerReplacePreservesRetainedState(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	removed := url.URL{Scheme: "http", Host: "removed.example"}
	added := url.URL{Scheme: "http", Host: "added.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{first, second, removed},
		2,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}

	firstPermit, ok := tracker.acquirePermit(first)
	if !ok {
		t.Fatal("first acquirePermit() allowed = false, want true")
	}
	tracker.recordOutcome(firstPermit, passiveHealthOutcomeFailure)
	firstPermit, ok = tracker.acquirePermit(first)
	if !ok {
		t.Fatal("second first-endpoint acquirePermit() allowed = false, want true")
	}
	if !tracker.recordOutcome(firstPermit, passiveHealthOutcomeFailure) {
		t.Fatal("second first-endpoint failure did not eject endpoint")
	}
	secondPermit, ok := tracker.acquirePermit(second)
	if !ok {
		t.Fatal("second acquirePermit() allowed = false, want true")
	}
	tracker.recordOutcome(secondPermit, passiveHealthOutcomeFailure)

	firstState := tracker.stateByTarget[first.String()]
	secondState := tracker.stateByTarget[second.String()]
	replacement := []url.URL{second, added, second, first}
	if err := tracker.Replace(replacement); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	replacement[0].Host = "mutated.example"

	wantOrdered := []url.URL{second, added, second, first}
	if !slices.Equal(tracker.ordered, wantOrdered) {
		t.Errorf("ordered endpoints = %v, want %v", tracker.ordered, wantOrdered)
	}
	if tracker.stateByTarget[first.String()] != firstState {
		t.Error("Replace() discarded retained first endpoint state")
	}
	if tracker.stateByTarget[second.String()] != secondState {
		t.Error("Replace() discarded retained second endpoint state")
	}
	if firstState.ejectionUntil.IsZero() {
		t.Error("Replace() cleared retained endpoint ejection")
	}
	if secondState.consecutiveFailures != 1 {
		t.Errorf(
			"retained consecutive failures = %d, want 1",
			secondState.consecutiveFailures,
		)
	}
	if _, exists := tracker.stateByTarget[removed.String()]; exists {
		t.Error("Replace() retained removed endpoint state")
	}
	addedState := tracker.stateByTarget[added.String()]
	if addedState == nil {
		t.Fatal("added endpoint state = nil")
	}
	if addedState.consecutiveFailures != 0 || !addedState.ejectionUntil.IsZero() {
		t.Errorf("added endpoint state = %+v, want clean state", addedState)
	}
	wantHealthy := []url.URL{second, added, second}
	if got := tracker.HealthyEndpoints(); !slices.Equal(got, wantHealthy) {
		t.Errorf("healthy endpoints = %v, want %v", got, wantHealthy)
	}
}

func TestPassiveHealthTrackerReplaceRejectsEmptySnapshotWithoutMutation(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 1, time.Minute)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	wantState := tracker.stateByTarget[target.String()]

	err = tracker.Replace(nil)

	if err == nil {
		t.Fatal("Replace() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "no endpoints") {
		t.Errorf("Replace() error = %q, want no endpoints context", err)
	}
	if !slices.Equal(tracker.ordered, []url.URL{target}) {
		t.Errorf("ordered endpoints = %v, want original endpoint", tracker.ordered)
	}
	if tracker.stateByTarget[target.String()] != wantState {
		t.Error("Replace() changed state after validation failure")
	}
}

func TestPassiveHealthTrackerIgnoresPermitFromRemovedAndReaddedEndpoint(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	other := url.URL{Scheme: "http", Host: "other.example"}
	tracker, err := NewPassiveHealthTracker([]url.URL{target}, 1, time.Minute)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	stalePermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("acquirePermit() allowed = false, want true")
	}
	if err := tracker.Replace([]url.URL{other}); err != nil {
		t.Fatalf("Replace() removing target error = %v", err)
	}
	if err := tracker.Replace([]url.URL{target}); err != nil {
		t.Fatalf("Replace() readding target error = %v", err)
	}
	currentState := tracker.stateByTarget[target.String()]
	if currentState == stalePermit.state {
		t.Fatal("readded endpoint reused removed state")
	}

	if tracker.recordOutcome(stalePermit, passiveHealthOutcomeFailure) {
		t.Error("stale permit ejected readded endpoint")
	}
	if currentState.consecutiveFailures != 0 || !currentState.ejectionUntil.IsZero() {
		t.Errorf("stale permit changed readded endpoint state: %+v", currentState)
	}
	currentPermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("readded endpoint acquirePermit() allowed = false, want true")
	}
	if !tracker.recordOutcome(currentPermit, passiveHealthOutcomeFailure) {
		t.Error("current permit failure did not eject readded endpoint")
	}
}

func TestPassiveHealthTrackerEjectsAndRecoversEndpoint(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{target},
		2,
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return now }

	firstPermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("first acquirePermit() allowed = false, want true")
	}
	secondPermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("second acquirePermit() allowed = false, want true")
	}
	if firstPermit.generation != secondPermit.generation {
		t.Errorf(
			"healthy permit generations = (%d, %d), want equal",
			firstPermit.generation,
			secondPermit.generation,
		)
	}
	if ejected := tracker.recordOutcome(firstPermit, passiveHealthOutcomeFailure); ejected {
		t.Error("first failure ejected endpoint, want endpoint available")
	}
	if ejected := tracker.recordOutcome(secondPermit, passiveHealthOutcomeFailure); !ejected {
		t.Error("second failure did not eject endpoint")
	}

	state := tracker.stateByTarget[target.String()]
	wantEjectionUntil := now.Add(30 * time.Second)
	if !state.ejectionUntil.Equal(wantEjectionUntil) {
		t.Errorf("ejection deadline = %s, want %s", state.ejectionUntil, wantEjectionUntil)
	}
	if _, ok := tracker.acquirePermit(target); ok {
		t.Error("acquirePermit() allowed endpoint during ejection")
	}

	now = wantEjectionUntil
	recoveredPermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("acquirePermit() at ejection deadline allowed = false, want true")
	}
	if !state.ejectionUntil.IsZero() {
		t.Errorf("ejection deadline after recovery = %s, want zero", state.ejectionUntil)
	}
	if recoveredPermit.generation != secondPermit.generation+2 {
		t.Errorf(
			"recovered generation = %d, want %d",
			recoveredPermit.generation,
			secondPermit.generation+2,
		)
	}
}

func TestPassiveHealthTrackerIgnoresStaleOutcomeAfterEjection(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return now }

	failingPermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("failing acquirePermit() allowed = false, want true")
	}
	lateSuccessPermit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("late-success acquirePermit() allowed = false, want true")
	}
	if !tracker.recordOutcome(failingPermit, passiveHealthOutcomeFailure) {
		t.Fatal("failure did not eject endpoint")
	}
	if tracker.recordOutcome(lateSuccessPermit, passiveHealthOutcomeSuccess) {
		t.Error("stale success reported a new ejection")
	}

	state := tracker.stateByTarget[target.String()]
	if state.ejectionUntil.IsZero() {
		t.Error("stale success cleared endpoint ejection")
	}
	if _, ok := tracker.acquirePermit(target); ok {
		t.Error("stale success made ejected endpoint available")
	}
}

func TestPassiveHealthTrackerSuccessResetsConsecutiveFailures(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{target},
		2,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}

	permit, ok := tracker.acquirePermit(target)
	if !ok {
		t.Fatal("acquirePermit() allowed = false, want true")
	}
	tracker.recordOutcome(permit, passiveHealthOutcomeFailure)
	tracker.recordOutcome(permit, passiveHealthOutcomeIgnored)
	tracker.recordOutcome(permit, passiveHealthOutcomeSuccess)
	if got := tracker.stateByTarget[target.String()].consecutiveFailures; got != 0 {
		t.Errorf("consecutive failures = %d, want 0", got)
	}
}

func TestPassiveHealthTrackerIgnoresUnknownTarget(t *testing.T) {
	t.Parallel()

	tracked := url.URL{Scheme: "http", Host: "tracked.example"}
	unknown := url.URL{Scheme: "http", Host: "unknown.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{tracked},
		1,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}

	if _, ok := tracker.acquirePermit(unknown); ok {
		t.Error("acquirePermit() allowed unknown target")
	}
	if tracker.recordOutcome(
		passiveHealthPermit{target: unknown.String()},
		passiveHealthOutcomeFailure,
	) {
		t.Error("recordOutcome() ejected unknown target")
	}
}

func TestPassiveHealthTrackerHealthyEndpointsFiltersAndRecoversDuplicates(
	t *testing.T,
) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{first, second, first},
		1,
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	clockCalls := 0
	tracker.now = func() time.Time {
		clockCalls++
		return now
	}
	permit, ok := tracker.acquirePermit(first)
	if !ok {
		t.Fatal("acquirePermit() allowed = false, want true")
	}
	if !tracker.recordOutcome(permit, passiveHealthOutcomeFailure) {
		t.Fatal("failure did not eject endpoint")
	}

	clockCalls = 0
	got := tracker.HealthyEndpoints()
	if want := []url.URL{second}; !slices.Equal(got, want) {
		t.Errorf("healthy endpoints during ejection = %v, want %v", got, want)
	}
	if clockCalls != 1 {
		t.Errorf("clock calls during snapshot = %d, want 1", clockCalls)
	}
	got[0].Host = "mutated.example"
	if next := tracker.HealthyEndpoints(); !slices.Equal(next, []url.URL{second}) {
		t.Errorf("mutating returned snapshot changed tracker endpoints to %v", next)
	}

	now = now.Add(30 * time.Second)
	clockCalls = 0
	got = tracker.HealthyEndpoints()
	want := []url.URL{first, second, first}
	if !slices.Equal(got, want) {
		t.Errorf("healthy endpoints after recovery = %v, want %v", got, want)
	}
	if clockCalls != 1 {
		t.Errorf("clock calls during recovery snapshot = %d, want 1", clockCalls)
	}
	state := tracker.stateByTarget[first.String()]
	if state.generation != 2 {
		t.Errorf("recovered endpoint generation = %d, want 2", state.generation)
	}
}

func TestPassiveHealthTrackerNextRecoveryDelayWithoutEjections(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{target},
		1,
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}

	delay, pending := tracker.NextRecoveryDelay()

	if pending {
		t.Error("NextRecoveryDelay() pending = true, want false")
	}
	if delay != 0 {
		t.Errorf("NextRecoveryDelay() delay = %s, want 0", delay)
	}
}

func TestPassiveHealthTrackerNextRecoveryDelayUsesEarliestDeadline(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	tracker, err := NewPassiveHealthTracker(
		[]url.URL{first, second},
		1,
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("NewPassiveHealthTracker() error = %v", err)
	}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	clockCalls := 0
	tracker.now = func() time.Time {
		clockCalls++
		return now
	}

	firstPermit, ok := tracker.acquirePermit(first)
	if !ok {
		t.Fatal("first acquirePermit() allowed = false, want true")
	}
	if !tracker.recordOutcome(firstPermit, passiveHealthOutcomeFailure) {
		t.Fatal("first endpoint failure did not eject endpoint")
	}
	now = now.Add(10 * time.Second)
	secondPermit, ok := tracker.acquirePermit(second)
	if !ok {
		t.Fatal("second acquirePermit() allowed = false, want true")
	}
	if !tracker.recordOutcome(secondPermit, passiveHealthOutcomeFailure) {
		t.Fatal("second endpoint failure did not eject endpoint")
	}

	clockCalls = 0
	delay, pending := tracker.NextRecoveryDelay()
	if !pending {
		t.Fatal("NextRecoveryDelay() pending = false, want true")
	}
	if delay != 20*time.Second {
		t.Errorf("NextRecoveryDelay() delay = %s, want 20s", delay)
	}
	if clockCalls != 1 {
		t.Errorf("NextRecoveryDelay() clock calls = %d, want 1", clockCalls)
	}

	now = now.Add(21 * time.Second)
	delay, pending = tracker.NextRecoveryDelay()
	if !pending {
		t.Fatal("NextRecoveryDelay() after deadline pending = false, want true")
	}
	if delay != 0 {
		t.Errorf("NextRecoveryDelay() after deadline delay = %s, want 0", delay)
	}

	wantHealthy := []url.URL{first}
	if got := tracker.HealthyEndpoints(); !slices.Equal(got, wantHealthy) {
		t.Errorf("HealthyEndpoints() after first recovery = %v, want %v", got, wantHealthy)
	}
	delay, pending = tracker.NextRecoveryDelay()
	if !pending {
		t.Fatal("NextRecoveryDelay() for remaining endpoint pending = false, want true")
	}
	if delay != 9*time.Second {
		t.Errorf("NextRecoveryDelay() for remaining endpoint delay = %s, want 9s", delay)
	}
}
