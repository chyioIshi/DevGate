package directresponse

import (
	"fmt"
	"io"
	"net/http"
)

// New returns an HTTP handler that writes the configured static response.
// The optional transform is applied to response headers before they are sent.
func New(statusCode int, body string, transform func(http.Header)) (http.Handler, error) {
	if statusCode < 200 || statusCode > 599 {
		return nil, fmt.Errorf("invalid status code: %d", statusCode)
	}
	if body != "" {
		switch statusCode {
		case http.StatusNoContent,
			http.StatusResetContent,
			http.StatusNotModified:
			return nil, fmt.Errorf("response status %d must not include a body", statusCode)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body != "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		if transform != nil {
			transform(w.Header())
		}
		w.WriteHeader(statusCode)
		if body == "" || r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, body)
	}), nil
}
