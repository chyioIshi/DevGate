package requesttimeout

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// New creates an HTTP handler that applies timeout to each request before
// delegating it to next. It returns an error if next is nil or timeout is not
// positive.
func New(next http.Handler, timeout time.Duration) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("next handler must not be nil")
	}
	if timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(
			r.Context(),
			timeout,
		)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	}), nil
}
