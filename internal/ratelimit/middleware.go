package ratelimit

import "net/http"

// Limiter decides whether one request may proceed immediately.
type Limiter interface {
	Allow() bool
}

// Observer records the outcome of rate limiter decisions.
type Observer interface {
	RecordAllowedRequest()
	RecordRejectedRequest()
}

var _ Limiter = (*Local)(nil)

// Middleware rejects requests that exceed limiter with HTTP 429 and delegates
// allowed requests to next.
func Middleware(next http.Handler, limiter Limiter, observer Observer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isAllowed := limiter.Allow()
		if !isAllowed {
			observer.RecordRejectedRequest()
			http.Error(
				w,
				http.StatusText(http.StatusTooManyRequests),
				http.StatusTooManyRequests,
			)
			return
		}
		observer.RecordAllowedRequest()
		next.ServeHTTP(w, r)
	})
}
