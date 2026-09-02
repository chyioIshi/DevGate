package proxy

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryTransport is an HTTP transport that retries eligible requests up to the configured maximum number of attempts.
type RetryTransport struct {
	base        http.RoundTripper
	baseDelay   time.Duration
	maxAttempts int
}

// NewRetryTransport creates a RetryTransport that makes at most maxAttempts attempts per request.
func NewRetryTransport(base http.RoundTripper, maxAttempts int, baseDelay time.Duration) (*RetryTransport, error) {
	if base == nil {
		return nil, errors.New("base transport must not be nil")
	}
	if maxAttempts < 1 {
		return nil, errors.New("max attempts must be positive")
	}
	if baseDelay <= 0 {
		return nil, errors.New("retry base delay must be positive")
	}
	retryTransport := &RetryTransport{
		base:        base,
		maxAttempts: maxAttempts,
		baseDelay:   baseDelay,
	}
	return retryTransport, nil
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if shouldRetry(req, err) {
		for retryIndex := range t.maxAttempts - 1 {
			maxDelay := retryDelay(t.baseDelay, retryIndex)
			delay := jitterDelay(maxDelay)
			if waitErr := waitForRetry(req.Context(), delay); waitErr != nil {
				return nil, waitErr
			}
			resp, err = t.base.RoundTrip(req)
			if !shouldRetry(req, err) {
				break
			}
		}
	}
	return resp, err
}

func shouldRetry(req *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if req.Context().Err() != nil {
		return false
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	if req.Body != nil && req.Body != http.NoBody {
		return false
	}
	return true
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryDelay(baseDelay time.Duration, retryIndex int) time.Duration {
	return baseDelay * (1 << retryIndex)
}

func jitterDelay(maxDelay time.Duration) time.Duration {
	jitter := rand.Int64N(int64(maxDelay))
	return time.Duration(jitter)
}
