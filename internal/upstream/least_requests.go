package upstream

import (
	"errors"
	"net/url"
	"sync"
)

// LeastRequests selects the endpoint with the fewest active requests and is
// safe for concurrent use.
type LeastRequests struct {
	mu        sync.Mutex
	endpoints []*leastRequestsEndpoint
	cursor    int
}

// NewLeastRequests creates a least-requests picker from a non-empty endpoint
// snapshot.
func NewLeastRequests(endpoints []url.URL) (*LeastRequests, error) {
	leastRequests := &LeastRequests{}
	if err := leastRequests.Replace(endpoints); err != nil {
		return nil, err
	}
	return leastRequests, nil
}

// Acquire returns the least-loaded endpoint and an idempotent function that
// releases its active-request permit.
func (l *LeastRequests) Acquire() (url.URL, func()) {
	l.mu.Lock()

	start := l.cursor % len(l.endpoints)
	selectedIndex := start
	selected := l.endpoints[start]

	for offset := 1; offset < len(l.endpoints); offset++ {
		index := (start + offset) % len(l.endpoints)
		candidate := l.endpoints[index]
		if candidate.activeRequests < selected.activeRequests {
			selectedIndex = index
			selected = candidate
		}
	}

	selected.activeRequests++
	l.cursor = (selectedIndex + 1) % len(l.endpoints)
	target := selected.target
	l.mu.Unlock()

	released := false
	release := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if released {
			return
		}
		released = true
		selected.activeRequests--
	}

	return target, release
}

// Replace atomically publishes a non-empty endpoint snapshot while preserving
// the active-request counts of unchanged endpoints.
func (l *LeastRequests) Replace(endpoints []url.URL) error {
	if len(endpoints) == 0 {
		return errors.New("no endpoints provided")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	existing := make(map[string][]*leastRequestsEndpoint, len(l.endpoints))
	for _, endpoint := range l.endpoints {
		key := endpoint.target.String()
		existing[key] = append(existing[key], endpoint)
	}

	replacement := make([]*leastRequestsEndpoint, len(endpoints))
	for i, target := range endpoints {
		key := target.String()
		matching := existing[key]
		if len(matching) == 0 {
			replacement[i] = &leastRequestsEndpoint{target: target}
			continue
		}

		state := matching[0]
		state.target = target
		replacement[i] = state
		existing[key] = matching[1:]
	}

	l.endpoints = replacement
	l.cursor %= len(l.endpoints)
	return nil
}

type leastRequestsEndpoint struct {
	target         url.URL
	activeRequests uint64
}
