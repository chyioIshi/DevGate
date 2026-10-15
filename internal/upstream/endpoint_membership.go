package upstream

import (
	"net/url"
	"slices"
	"sync/atomic"
)

// EndpointMembership publishes the current ordered endpoint membership as
// immutable snapshots and treats every current member as health-eligible.
type EndpointMembership struct {
	snapshot atomic.Pointer[endpointMembershipSnapshot]
}

type endpointMembershipSnapshot struct {
	endpoints []url.URL
}

var _ healthyEndpointSource = (*EndpointMembership)(nil)

// NewEndpointMembership creates membership from a copied endpoint snapshot.
func NewEndpointMembership(endpoints []url.URL) *EndpointMembership {
	em := &EndpointMembership{}
	em.Replace(endpoints)
	return em
}

// HealthyEndpoints returns a copy of the current membership snapshot.
func (e *EndpointMembership) HealthyEndpoints() []url.URL {
	snapshot := e.snapshot.Load()
	if snapshot == nil {
		return nil
	}
	return slices.Clone(snapshot.endpoints)
}

// Replace atomically publishes a copied endpoint snapshot. Empty snapshots are
// allowed so discovery can report that no endpoints currently exist.
func (e *EndpointMembership) Replace(endpoints []url.URL) {
	endpointsClone := slices.Clone(endpoints)
	newSnapshot := &endpointMembershipSnapshot{
		endpoints: endpointsClone,
	}
	e.snapshot.Store(newSnapshot)
}
