package router

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// LoadBalancingPolicy identifies how requests are distributed across upstream endpoints.
type LoadBalancingPolicy string

const (
	// LoadBalancingPolicyRoundRobin selects endpoints in cyclic order.
	LoadBalancingPolicyRoundRobin LoadBalancingPolicy = "round_robin"
	// LoadBalancingPolicyRandom selects endpoints uniformly at random.
	LoadBalancingPolicyRandom LoadBalancingPolicy = "random"
	// LoadBalancingPolicyLeastRequests selects the endpoint with the fewest active requests.
	LoadBalancingPolicyLeastRequests LoadBalancingPolicy = "least_requests"
)

// Upstream describes a group of backend endpoints and its load-balancing policy.
type Upstream struct {
	LoadBalancing LoadBalancingPolicy
	Endpoints     []url.URL
}

func (u *Upstream) validate() error {
	switch u.LoadBalancing {
	case LoadBalancingPolicyRoundRobin,
		LoadBalancingPolicyRandom,
		LoadBalancingPolicyLeastRequests:
	default:
		return fmt.Errorf(
			"unsupported load balancing policy %q",
			u.LoadBalancing,
		)
	}
	if len(u.Endpoints) == 0 {
		return errors.New("at least one upstream endpoint must be specified")
	}
	for i, endpoint := range u.Endpoints {
		if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
			return fmt.Errorf("upstream endpoint [%d]: URL must have a valid scheme - http or https", i)
		}
		if strings.TrimSpace(endpoint.Host) == "" {
			return fmt.Errorf("upstream endpoint [%d]: URL must have a valid host", i)
		}
	}
	return nil
}
