package redirect

import (
	"errors"
	"net/http"
)

// New returns an HTTP handler that redirects requests to location using the
// configured redirect status. The optional transform is applied to response
// headers before the redirect is written.
func New(statusCode int, location string, transform func(http.Header)) (http.Handler, error) {
	switch statusCode {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
	default:
		return nil, errors.New("redirect status code must be one of 301, 302, 303, 307, or 308")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if transform != nil {
			transform(w.Header())
		}
		http.Redirect(w, r, location, statusCode)
	}), nil
}
