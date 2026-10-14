package upstream

import (
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
)

// ErrNoHealthyEndpoints indicates that no currently healthy endpoint can
// accept a request.
var ErrNoHealthyEndpoints = errors.New("no healthy endpoints")

// HealthAwarePicker restricts target selection to the snapshot published by a
// healthy endpoint source.
type HealthAwarePicker struct {
	mu        sync.Mutex
	picker    replaceablePicker
	source    healthyEndpointSource
	available atomic.Bool
}

type replaceablePicker interface {
	Acquire() (url.URL, func(), error)
	Replace([]url.URL) error
}

type healthyEndpointSource interface {
	HealthyEndpoints() []url.URL
}

// NewHealthAwarePicker creates a picker and synchronizes it with the current
// health snapshot.
func NewHealthAwarePicker(
	picker replaceablePicker,
	source healthyEndpointSource,
) (*HealthAwarePicker, error) {
	if picker == nil {
		return nil, errors.New("picker was not provided")
	}
	if source == nil {
		return nil, errors.New("healthy endpoint source was not provided")
	}
	healthAwarePicker := &HealthAwarePicker{
		picker: picker,
		source: source,
	}
	if err := healthAwarePicker.Refresh(); err != nil {
		return nil, fmt.Errorf("refresh health-aware picker: %w", err)
	}
	return healthAwarePicker, nil
}

// Acquire delegates target selection when at least one healthy endpoint is
// available.
func (p *HealthAwarePicker) Acquire() (url.URL, func(), error) {
	available := p.available.Load()
	if !available {
		return url.URL{}, nil, ErrNoHealthyEndpoints
	}
	return p.picker.Acquire()
}

// Refresh publishes the source's current healthy endpoint snapshot. An empty
// snapshot closes the picker without replacing its last non-empty snapshot.
func (p *HealthAwarePicker) Refresh() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.available.Store(false)
	healthyEndpoints := p.source.HealthyEndpoints()
	if len(healthyEndpoints) == 0 {
		return nil
	}
	err := p.picker.Replace(healthyEndpoints)
	if err != nil {
		return fmt.Errorf("replace healthy endpoints: %w", err)
	}
	p.available.Store(true)
	return nil
}
