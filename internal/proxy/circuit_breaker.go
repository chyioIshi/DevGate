package proxy

import (
	"errors"
	"net/http"
	"sync"
	"time"
)

type circuitState int

const (
	circuitClosed circuitState = iota
	circuitOpen
	circuitHalfOpen
)

type circuitOutcome int

const (
	circuitOutcomeIgnored circuitOutcome = iota
	circuitOutcomeSuccess
	circuitOutcomeFailure
)

// ErrCircuitOpen indicates that the circuit breaker rejected a request without
// contacting the upstream.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreakerTransport wraps an HTTP transport and temporarily rejects
// requests after repeated upstream failures.
type CircuitBreakerTransport struct {
	base             http.RoundTripper
	failureThreshold int
	openTimeout      time.Duration
	now              func() time.Time

	// mu guards generation, state, consecutiveFailures, and openedAt.
	mu                  sync.Mutex
	generation          uint64
	state               circuitState
	consecutiveFailures int
	openedAt            time.Time
}

var _ http.RoundTripper = (*CircuitBreakerTransport)(nil)

type circuitPermit struct {
	generation uint64
	probe      bool
}

// NewCircuitBreakerTransport creates a circuit breaker that delegates allowed
// requests to base.
func NewCircuitBreakerTransport(
	base http.RoundTripper,
	failureThreshold int,
	openTimeout time.Duration,
) (*CircuitBreakerTransport, error) {
	if base == nil {
		return nil, errors.New("base transport must not be nil")
	}
	if failureThreshold < 1 {
		return nil, errors.New("failure threshold must be positive")
	}
	if openTimeout <= 0 {
		return nil, errors.New("circuit open timeout must be positive")
	}
	return &CircuitBreakerTransport{
		base:             base,
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
		now:              time.Now,
	}, nil
}

// RoundTrip executes an allowed request and records its outcome in the circuit
// breaker state.
func (t *CircuitBreakerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	permit, err := t.acquirePermit()
	if err != nil {
		return nil, err
	}

	resp, err := t.base.RoundTrip(req)
	outcome := classifyCircuitOutcome(req, resp, err)
	t.recordOutcome(permit, outcome)

	return resp, err
}

func (t *CircuitBreakerTransport) acquirePermit() (circuitPermit, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	switch t.state {
	case circuitClosed:
		return circuitPermit{generation: t.generation, probe: false}, nil
	case circuitOpen:
		now := t.now()
		if now.Sub(t.openedAt) < t.openTimeout {
			return circuitPermit{}, ErrCircuitOpen
		}
		t.state = circuitHalfOpen
		return circuitPermit{generation: t.generation, probe: true}, nil
	case circuitHalfOpen:
		return circuitPermit{}, ErrCircuitOpen
	default:
		return circuitPermit{}, ErrCircuitOpen
	}
}

func (t *CircuitBreakerTransport) recordOutcome(p circuitPermit, outcome circuitOutcome) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if p.generation != t.generation {
		return
	}

	if p.probe {
		if t.state != circuitHalfOpen {
			return
		}

		switch outcome {
		case circuitOutcomeSuccess:
			t.state = circuitClosed
			t.openedAt = time.Time{}
		case circuitOutcomeFailure:
			t.state = circuitOpen
			t.openedAt = t.now()
		case circuitOutcomeIgnored:
			t.state = circuitOpen
		default:
			return
		}
		t.consecutiveFailures = 0
		t.generation++

		return
	}

	if t.state != circuitClosed {
		return
	}

	switch outcome {
	case circuitOutcomeSuccess:
		t.consecutiveFailures = 0
	case circuitOutcomeFailure:
		t.consecutiveFailures++

		if t.consecutiveFailures >= t.failureThreshold {
			t.state = circuitOpen
			t.openedAt = t.now()
			t.consecutiveFailures = 0
			t.generation++
		}
	case circuitOutcomeIgnored:
		return
	}
}

func classifyCircuitOutcome(
	req *http.Request,
	resp *http.Response,
	err error,
) circuitOutcome {
	if req.Context().Err() != nil {
		return circuitOutcomeIgnored
	}
	if err != nil {
		return circuitOutcomeFailure
	}
	if resp == nil {
		return circuitOutcomeIgnored
	}
	if http.StatusInternalServerError <= resp.StatusCode &&
		resp.StatusCode < 600 {
		return circuitOutcomeFailure
	}

	return circuitOutcomeSuccess
}
