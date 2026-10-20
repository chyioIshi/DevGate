package upstream

import (
	"errors"
	"fmt"
	"net/url"
	"sync"
)

// EndpointCoordinator applies endpoint snapshots to membership, health state,
// and target selection in a consistent order.
type EndpointCoordinator struct {
	mu                   sync.Mutex
	endpointMembership   *EndpointMembership
	picker               *HealthAwarePicker
	activeHealthTracker  *HealthTracker
	passiveHealthTracker *PassiveHealthTracker
}

// NewEndpointCoordinator creates a coordinator with required membership and
// picker dependencies and optional active and passive health trackers.
func NewEndpointCoordinator(
	endpointMembership *EndpointMembership,
	picker *HealthAwarePicker,
	activeHealthTracker *HealthTracker,
	passiveHealthTracker *PassiveHealthTracker,
) (*EndpointCoordinator, error) {
	if endpointMembership == nil {
		return nil, errors.New("endpoint membership was not provided")
	}
	if picker == nil {
		return nil, errors.New("health-aware picker was not provided")
	}
	return &EndpointCoordinator{
		endpointMembership:   endpointMembership,
		picker:               picker,
		activeHealthTracker:  activeHealthTracker,
		passiveHealthTracker: passiveHealthTracker,
	}, nil
}

// Apply serializes publication of an endpoint snapshot across health trackers,
// membership, and the health-aware picker. Empty snapshots are allowed.
func (c *EndpointCoordinator) Apply(endpoints []url.URL) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeHealthTracker != nil {
		if err := c.activeHealthTracker.Replace(endpoints); err != nil {
			return fmt.Errorf("replace active health endpoints: %w", err)
		}
	}
	if c.passiveHealthTracker != nil {
		if err := c.passiveHealthTracker.Replace(endpoints); err != nil {
			return fmt.Errorf("replace passive health endpoints: %w", err)
		}
	}

	c.endpointMembership.Replace(endpoints)
	if err := c.picker.Refresh(); err != nil {
		return fmt.Errorf("refresh health-aware picker: %w", err)
	}
	return nil
}
