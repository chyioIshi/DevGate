package router

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
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
	LoadBalancing     LoadBalancingPolicy
	Endpoints         []url.URL
	ActiveHealthCheck *ActiveHealthCheckPolicy
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
	if u.ActiveHealthCheck != nil {
		if err := u.ActiveHealthCheck.validate(); err != nil {
			return fmt.Errorf("active health check validation: %w", err)
		}
	}
	return nil
}

// ActiveHealthCheckPolicy defines periodic HTTP probing and state-transition
// thresholds for an upstream pool.
type ActiveHealthCheckPolicy struct {
	Path                string
	Interval            time.Duration
	Timeout             time.Duration
	HealthyThreshold    int
	UnhealthyThreshold  int
	MaxConcurrentProbes int
}

func (p *ActiveHealthCheckPolicy) validate() error {
	if strings.TrimSpace(p.Path) == "" {
		return errors.New("path must be provided")
	}
	if !strings.HasPrefix(p.Path, "/") {
		return errors.New("path must start with '/'")
	}
	if strings.ContainsAny(p.Path, "?#") {
		return errors.New("path must not contain URL reserved characters")
	}
	if p.Interval <= 0 {
		return errors.New("interval must be positive")
	}
	if p.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if p.HealthyThreshold <= 0 {
		return errors.New("healthy threshold must be positive")
	}
	if p.UnhealthyThreshold <= 0 {
		return errors.New("unhealthy threshold must be positive")
	}
	if p.MaxConcurrentProbes <= 0 {
		return errors.New("max concurrent probes must be positive")
	}
	return nil
}
