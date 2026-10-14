package upstream

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/chyioishi/devgate/internal/proxytarget"
)

var errPassiveHealthTargetMissing = errors.New(
	"selected upstream target missing from request context",
)

// PassiveHealthObserver observes endpoints ejected after data-plane failures.
// Implementations must be safe for concurrent use and return promptly.
type PassiveHealthObserver interface {
	RecordEjection(target url.URL)
}

// PassiveHealthTransport observes proxied request outcomes and updates passive
// health state before returning the base transport's result.
type PassiveHealthTransport struct {
	base     http.RoundTripper
	tracker  *PassiveHealthTracker
	observer PassiveHealthObserver
}

var _ http.RoundTripper = (*PassiveHealthTransport)(nil)

// NewPassiveHealthTransport creates a passive-health wrapper around base.
func NewPassiveHealthTransport(
	base http.RoundTripper,
	tracker *PassiveHealthTracker,
	observer PassiveHealthObserver,
) (*PassiveHealthTransport, error) {
	if base == nil {
		return nil, errors.New("base transport cannot be nil")
	}
	if tracker == nil {
		return nil, errors.New("tracker cannot be nil")
	}
	if observer == nil {
		return nil, errors.New("observer cannot be nil")
	}
	return &PassiveHealthTransport{
		base:     base,
		tracker:  tracker,
		observer: observer,
	}, nil
}

// RoundTrip delegates an available target's request and records its outcome.
func (t *PassiveHealthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, ok := proxytarget.FromContext(req.Context())
	if !ok {
		return nil, errPassiveHealthTargetMissing
	}
	permit, ok := t.tracker.acquirePermit(target)
	if !ok {
		return nil, ErrNoHealthyEndpoints
	}
	resp, err := t.base.RoundTrip(req)
	outcome := classifyPassiveHealthOutcome(req, resp, err)
	ejected := t.tracker.recordOutcome(permit, outcome)
	if ejected {
		t.observer.RecordEjection(target)
	}
	return resp, err
}

func classifyPassiveHealthOutcome(
	req *http.Request,
	resp *http.Response,
	err error,
) passiveHealthOutcome {
	if req.Context().Err() != nil {
		return passiveHealthOutcomeIgnored
	}
	if err != nil {
		return passiveHealthOutcomeFailure
	}
	if resp == nil {
		return passiveHealthOutcomeIgnored
	}
	if resp.StatusCode >= 500 && resp.StatusCode < 600 {
		return passiveHealthOutcomeFailure
	}
	return passiveHealthOutcomeSuccess
}
