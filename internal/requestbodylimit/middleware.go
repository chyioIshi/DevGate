package requestbodylimit

import (
	"errors"
	"net/http"
)

// New creates an HTTP handler that limits each request body to maxBytes. It
// rejects a known Content-Length above the limit before delegating to next;
// otherwise, reading beyond the limit returns an *http.MaxBytesError. New
// returns an error if next is nil or maxBytes is not positive.
func New(next http.Handler, maxBytes int64) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("next handler must not be nil")
	}
	if maxBytes <= 0 {
		return nil, errors.New("max bytes must be positive")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBytes {
			http.Error(
				w,
				http.StatusText(http.StatusRequestEntityTooLarge),
				http.StatusRequestEntityTooLarge,
			)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	}), nil
}
