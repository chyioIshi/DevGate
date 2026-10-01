package upstream

import (
	"errors"
	"net/url"
	"slices"
	"sync/atomic"
)

// RoundRobin selects upstream endpoints in cyclic order and is safe for
// concurrent use.
type RoundRobin struct {
	snapshot atomic.Pointer[endpointSnapshot]
	next     atomic.Uint64
}

// NewRoundRobin creates a picker from a non-empty snapshot of endpoints.
func NewRoundRobin(endpoints []url.URL) (*RoundRobin, error) {
	roundRobin := &RoundRobin{}
	err := roundRobin.Replace(endpoints)
	if err != nil {
		return nil, err
	}
	return roundRobin, nil
}

// Next returns the next endpoint in the round-robin sequence.
func (r *RoundRobin) Next() url.URL {
	idx := r.next.Add(1) - 1
	snapshot := r.snapshot.Load()
	return snapshot.endpoints[idx%uint64(len(snapshot.endpoints))]
}

// Replace atomically publishes a copied, non-empty snapshot of endpoints.
// When validation fails, the current snapshot remains active.
func (r *RoundRobin) Replace(endpoints []url.URL) error {
	if len(endpoints) == 0 {
		return errors.New("no endpoints provided")
	}
	endpointsSnapshot := &endpointSnapshot{
		endpoints: slices.Clone(endpoints),
	}
	r.snapshot.Store(endpointsSnapshot)
	return nil
}

type endpointSnapshot struct {
	endpoints []url.URL
}
