package upstream

import (
	"errors"
	"math/rand/v2"
	"net/url"
	"slices"
	"sync/atomic"
)

// Random selects upstream endpoints uniformly at random and is safe for
// concurrent use.
type Random struct {
	snapshot atomic.Pointer[endpointSnapshot]
}

// NewRandom creates a random picker from a non-empty snapshot of endpoints.
func NewRandom(endpoints []url.URL) (*Random, error) {
	r := &Random{}
	if err := r.Replace(endpoints); err != nil {
		return nil, err
	}
	return r, nil
}

// Acquire returns a randomly selected endpoint and its release function.
func (r *Random) Acquire() (url.URL, func(), error) {
	snapshot := r.snapshot.Load()
	idx := rand.IntN(len(snapshot.endpoints))
	return snapshot.endpoints[idx], func() {}, nil
}

// Replace atomically publishes a copied, non-empty snapshot of endpoints.
// When validation fails, the current snapshot remains active.
func (r *Random) Replace(endpoints []url.URL) error {
	if len(endpoints) == 0 {
		return errors.New("no endpoints provided")
	}
	endpointsSnapshot := &endpointSnapshot{
		endpoints: slices.Clone(endpoints),
	}
	r.snapshot.Store(endpointsSnapshot)
	return nil
}
