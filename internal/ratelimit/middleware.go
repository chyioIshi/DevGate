package ratelimit

import (
	"context"
	"net/http"
)

// Limiter decides whether one request may proceed immediately.
// It returns an error when the decision cannot be made reliably.
type Limiter interface {
	Allow(ctx context.Context) (bool, error)
}

var _ Limiter = (*Local)(nil)

// Observer records the outcome of rate limiter decisions.
type Observer interface {
	RecordAllowedRequest()
	RecordRejectedRequest()
	RecordFailedRequest()
}

// ErrorResponder writes an HTTP error produced by rate-limit middleware.
type ErrorResponder func(
	http.ResponseWriter,
	*http.Request,
	int,
)

// Middleware delegates allowed requests to next, rejects denied requests with
// HTTP 429, and fails closed with HTTP 503 when limiter cannot make a decision.
func Middleware(
	next http.Handler,
	limiter Limiter,
	observer Observer,
	errorResponder ErrorResponder,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		isAllowed, err := limiter.Allow(ctx)
		if err != nil {
			observer.RecordFailedRequest()
			if errorResponder != nil {
				errorResponder(w, r, http.StatusServiceUnavailable)
			} else {
				http.Error(
					w,
					http.StatusText(http.StatusServiceUnavailable),
					http.StatusServiceUnavailable,
				)
			}
			return
		}
		if !isAllowed {
			observer.RecordRejectedRequest()
			if errorResponder != nil {
				errorResponder(w, r, http.StatusTooManyRequests)
			} else {
				http.Error(
					w,
					http.StatusText(http.StatusTooManyRequests),
					http.StatusTooManyRequests,
				)
			}
			return
		}
		observer.RecordAllowedRequest()
		next.ServeHTTP(w, r)
	})
}
