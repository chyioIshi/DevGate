package upstream

import (
	"errors"
	"net/url"
)

type endpointHealth struct {
	target  url.URL
	healthy bool

	consecutiveSuccesses int
	successThreshold     int
	consecutiveFailures  int
	failureThreshold     int
}

func newEndpointHealth(
	target url.URL,
	successThreshold int,
	failureThreshold int,
) (*endpointHealth, error) {
	if successThreshold <= 0 {
		return nil, errors.New("success threshold must be greater than 0")
	}
	if failureThreshold <= 0 {
		return nil, errors.New("failure threshold must be greater than 0")
	}
	return &endpointHealth{
		target:           target,
		healthy:          true,
		successThreshold: successThreshold,
		failureThreshold: failureThreshold,
	}, nil
}

func (h *endpointHealth) isHealthy() bool {
	return h.healthy
}

func (h *endpointHealth) recordSuccess() bool {
	h.consecutiveFailures = 0
	if h.healthy {
		return false
	}
	h.consecutiveSuccesses++
	if h.consecutiveSuccesses >= h.successThreshold {
		h.healthy = true
		h.consecutiveSuccesses = 0
		return true
	}
	return false
}

func (h *endpointHealth) recordFailure() bool {
	h.consecutiveSuccesses = 0
	if !h.healthy {
		return false
	}
	h.consecutiveFailures++
	if h.consecutiveFailures >= h.failureThreshold {
		h.healthy = false
		h.consecutiveFailures = 0
		return true
	}
	return false
}
