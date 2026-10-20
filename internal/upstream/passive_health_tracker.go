package upstream

import (
	"errors"
	"net/url"
	"slices"
	"sync"
	"time"
)

// PassiveHealthTracker tracks consecutive data-plane failures and temporary
// ejection state for upstream endpoints.
type PassiveHealthTracker struct {
	mu               sync.Mutex
	ordered          []url.URL
	stateByTarget    map[string]*passiveEndpointHealth
	failureThreshold int
	ejectionDuration time.Duration
	now              func() time.Time
}

type passiveEndpointHealth struct {
	target              url.URL
	consecutiveFailures int
	ejectionUntil       time.Time
	generation          uint64
}

type passiveHealthPermit struct {
	target     string
	state      *passiveEndpointHealth
	generation uint64
}

type passiveHealthOutcome int

const (
	passiveHealthOutcomeIgnored passiveHealthOutcome = iota
	passiveHealthOutcomeFailure
	passiveHealthOutcomeSuccess
)

// NewPassiveHealthTracker creates a tracker for a non-empty endpoint set using
// a positive failure threshold and ejection duration.
func NewPassiveHealthTracker(
	endpoints []url.URL,
	failureThreshold int,
	ejectionDuration time.Duration,
) (*PassiveHealthTracker, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("no endpoints provided")
	}
	if failureThreshold <= 0 {
		return nil, errors.New("failure threshold must be greater than 0")
	}
	if ejectionDuration <= 0 {
		return nil, errors.New("ejection duration must be greater than 0")
	}
	endpointsClone := slices.Clone(endpoints)
	stateByTarget := make(map[string]*passiveEndpointHealth, len(endpoints))
	for _, endpoint := range endpointsClone {
		key := endpoint.String()
		if _, exists := stateByTarget[key]; exists {
			continue
		}
		stateByTarget[key] = &passiveEndpointHealth{
			target: endpoint,
		}
	}
	passiveHealthTracker := &PassiveHealthTracker{
		ordered:          endpointsClone,
		stateByTarget:    stateByTarget,
		failureThreshold: failureThreshold,
		ejectionDuration: ejectionDuration,
		now:              time.Now,
	}
	return passiveHealthTracker, nil
}

// HealthyEndpoints returns an ordered snapshot excluding endpoints whose
// passive ejection period has not expired.
func (t *PassiveHealthTracker) HealthyEndpoints() []url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	healthy := make([]url.URL, 0, len(t.ordered))
	for _, endpoint := range t.ordered {
		key := endpoint.String()
		state, exists := t.stateByTarget[key]
		if !exists {
			continue
		}
		if t.endpointAvailableLocked(state, now) {
			healthy = append(healthy, endpoint)
		}
	}
	return healthy
}

// NextRecoveryDelay returns the time until the earliest ejected endpoint is
// eligible for recovery and reports whether any recovery is pending.
func (t *PassiveHealthTracker) NextRecoveryDelay() (
	time.Duration,
	bool,
) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var earliest time.Time

	for _, state := range t.stateByTarget {
		if state.ejectionUntil.IsZero() {
			continue
		}
		if earliest.IsZero() || state.ejectionUntil.Before(earliest) {
			earliest = state.ejectionUntil
		}
	}
	if earliest.IsZero() {
		return 0, false
	}

	now := t.now()
	delay := earliest.Sub(now)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

// Replace publishes a copied endpoint snapshot while preserving the passive
// health state of endpoints that remain in the set. Empty snapshots remove
// all tracked endpoints.
func (t *PassiveHealthTracker) Replace(endpoints []url.URL) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	endpointsClone := slices.Clone(endpoints)
	stateByTarget := make(map[string]*passiveEndpointHealth, len(endpointsClone))
	for _, endpoint := range endpointsClone {
		key := endpoint.String()
		if existing, exists := t.stateByTarget[key]; exists {
			existing.target = endpoint
			stateByTarget[key] = existing
			continue
		}
		stateByTarget[key] = &passiveEndpointHealth{
			target: endpoint,
		}
	}

	t.ordered = endpointsClone
	t.stateByTarget = stateByTarget
	return nil
}

func (t *PassiveHealthTracker) acquirePermit(
	target url.URL,
) (passiveHealthPermit, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := target.String()
	state, exists := t.stateByTarget[key]
	if !exists {
		return passiveHealthPermit{}, false
	}

	endpointAvailable := t.endpointAvailableLocked(state, t.now())
	if !endpointAvailable {
		return passiveHealthPermit{}, false
	}
	return passiveHealthPermit{
		target:     key,
		state:      state,
		generation: state.generation,
	}, true
}

func (t *PassiveHealthTracker) recordOutcome(
	permit passiveHealthPermit,
	outcome passiveHealthOutcome,
) (ejected bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.stateByTarget[permit.target]
	if !exists || state.generation != permit.generation || state != permit.state {
		return false
	}

	switch outcome {
	case passiveHealthOutcomeSuccess:
		state.consecutiveFailures = 0
	case passiveHealthOutcomeFailure:
		state.consecutiveFailures++
		if state.consecutiveFailures >= t.failureThreshold {
			state.ejectionUntil = t.now().Add(t.ejectionDuration)
			state.consecutiveFailures = 0
			state.generation++
			return true
		}
	case passiveHealthOutcomeIgnored:
	}
	return false
}

func (t *PassiveHealthTracker) endpointAvailableLocked(
	state *passiveEndpointHealth,
	now time.Time,
) bool {
	if state.ejectionUntil.IsZero() {
		return true
	}
	if now.Before(state.ejectionUntil) {
		return false
	}
	state.ejectionUntil = time.Time{}
	state.consecutiveFailures = 0
	state.generation++
	return true
}
