package ratelimit

import "net/http"

// Limiter decides whether one request may proceed immediately.
type Limiter interface {
	Allow() bool
}

var _ Limiter = (*Local)(nil)

// Observer records the outcome of rate limiter decisions.
type Observer interface {
	RecordAllowedRequest()
	RecordRejectedRequest()
}

// ErrorResponder writes an HTTP error for a rejected request.
type ErrorResponder func(
	http.ResponseWriter,
	*http.Request,
	int,
)

// Middleware rejects requests that exceed limiter with HTTP 429 and delegates
// allowed requests to next.
func Middleware(
	next http.Handler,
	limiter Limiter,
	observer Observer,
	errorResponder ErrorResponder,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAllowed := limiter.Allow()
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
