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
	endpoints []url.URL
	next      atomic.Uint64
}

// NewRoundRobin creates a picker from a non-empty snapshot of endpoints.
func NewRoundRobin(endpoints []url.URL) (*RoundRobin, error) {
	if len(endpoints) == 0 {
		return nil, errors.New("no endpoints provided")
	}
	return &RoundRobin{
		endpoints: slices.Clone(endpoints),
	}, nil
}

// Next returns the next endpoint in the round-robin sequence.
func (r *RoundRobin) Next() url.URL {
	idx := r.next.Add(1) - 1
	return r.endpoints[idx%uint64(len(r.endpoints))]
}
