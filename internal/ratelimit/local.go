package ratelimit

import (
	"errors"
	"math"

	"golang.org/x/time/rate"
)

// Local is a process-local token-bucket rate limiter safe for concurrent use.
type Local struct {
	limiter *rate.Limiter
}

// NewLocal creates a local rate limiter with the given refill rate and maximum
// burst size.
func NewLocal(requestsPerSecond float64, burst int) (*Local, error) {
	if requestsPerSecond <= 0 {
		return nil, errors.New("requests per second limit must be positive")
	}
	if math.IsInf(requestsPerSecond, 0) || math.IsNaN(requestsPerSecond) {
		return nil, errors.New("requests per second limit must be in a finite range")
	}
	if burst <= 0 {
		return nil, errors.New("burst must be positive")
	}
	limiter := rate.NewLimiter(
		rate.Limit(requestsPerSecond), burst,
	)
	return &Local{
		limiter: limiter,
	}, nil
}

// Allow reports whether one request may proceed immediately.
func (l *Local) Allow() bool {
	return l.limiter.Allow()
}
