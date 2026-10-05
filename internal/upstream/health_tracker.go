package upstream

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"
)

// HealthTracker owns the health state of an ordered set of upstream endpoints.
type HealthTracker struct {
	mu               sync.Mutex
	healthByTarget   map[string]*endpointHealth
	order            []url.URL
	successThreshold int
	failureThreshold int
}

// NewHealthTracker creates a tracker for a non-empty endpoint set using
// positive success and failure transition thresholds.
func NewHealthTracker(
	endpoints []url.URL,
	successThreshold int,
	failureThreshold int,
) (*HealthTracker, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("no endpoints provided")
	}
	if successThreshold <= 0 {
		return nil, errors.New("success threshold must be greater than 0")
	}
	if failureThreshold <= 0 {
		return nil, errors.New("failure threshold must be greater than 0")
	}
	endpointsClone := slices.Clone(endpoints)
	healthTrackerEndpoints := make(
		map[string]*endpointHealth,
		len(endpoints),
	)
	for _, endpoint := range endpointsClone {
		key := endpoint.String()
		if _, exists := healthTrackerEndpoints[key]; exists {
			continue
		}
		health, err := newEndpointHealth(
			endpoint,
			successThreshold,
			failureThreshold,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"create health state for endpoint %q: %w",
				key,
				err,
			)
		}
		healthTrackerEndpoints[key] = health
	}
	return &HealthTracker{
		healthByTarget:   healthTrackerEndpoints,
		order:            endpointsClone,
		successThreshold: successThreshold,
		failureThreshold: failureThreshold,
	}, nil
}

// RecordSuccess records a successful interaction with target and reports
// whether its health state changed and whether the target is tracked.
func (t *HealthTracker) RecordSuccess(target url.URL) (changed bool, tracked bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	healthState, tracked := t.healthByTarget[target.String()]

	if !tracked {
		return false, false
	}
	changed = healthState.recordSuccess()
	return changed, true
}

// RecordFailure records a failed interaction with target and reports whether
// its health state changed and whether the target is tracked.
func (t *HealthTracker) RecordFailure(target url.URL) (changed bool, tracked bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	healthState, tracked := t.healthByTarget[target.String()]

	if !tracked {
		return false, false
	}
	changed = healthState.recordFailure()
	return changed, true
}

// HealthyEndpoints returns an ordered snapshot containing only endpoints that
// are currently healthy.
func (t *HealthTracker) HealthyEndpoints() []url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()

	healthyEndpoints := make([]url.URL, 0, len(t.order))

	for _, endpoint := range t.order {
		state := t.healthByTarget[endpoint.String()]
		if state.isHealthy() {
			healthyEndpoints = append(healthyEndpoints, endpoint)
		}
	}
	return healthyEndpoints
}

// Replace publishes a copied, non-empty endpoint snapshot while preserving
// the health state of endpoints that remain in the set.
func (t *HealthTracker) Replace(endpoints []url.URL) error {
	if len(endpoints) == 0 {
		return errors.New("no endpoints provided")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	endpointsClone := slices.Clone(endpoints)
	newSnapshot := make(map[string]*endpointHealth, len(endpointsClone))
	for _, endpoint := range endpointsClone {
		key := endpoint.String()
		if healthState, exists := t.healthByTarget[key]; exists {
			healthState.target = endpoint
			newSnapshot[key] = healthState
		} else {
			health, err := newEndpointHealth(
				endpoint,
				t.successThreshold,
				t.failureThreshold,
			)
			if err != nil {
				return fmt.Errorf(
					"create health state for endpoint %q: %w",
					key,
					err,
				)
			}
			newSnapshot[key] = health
		}
	}

	t.healthByTarget = newSnapshot
	t.order = endpointsClone
	return nil
}
