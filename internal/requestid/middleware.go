package requestid

import (
	"context"
	"log/slog"
	"net/http"
)

type contextKey struct{}

type generatorFunc func() (string, error)

// HeaderName is the HTTP header used to propagate the gateway-generated request ID.
const HeaderName = "X-Request-ID"

var requestIDKey contextKey

// Middleware returns an HTTP handler that generates and propagates a request ID.
func Middleware(
	next http.Handler,
	logger *slog.Logger,
) http.Handler {
	return middleware(next, logger, Generate)
}

func middleware(next http.Handler, logger *slog.Logger, generate generatorFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := generate()
		if err != nil {
			logger.ErrorContext(r.Context(), "generate request ID", "error", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		ctx := context.WithValue(
			r.Context(),
			requestIDKey,
			requestID,
		)
		requestWithID := r.WithContext(ctx)
		requestWithID.Header.Set(HeaderName, requestID)
		w.Header().Set(HeaderName, requestID)
		next.ServeHTTP(w, requestWithID)
	})
}

// FromContext returns the request ID stored in ctx and reports whether it was present.
func FromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(requestIDKey)
	requestID, ok := value.(string)
	return requestID, ok
}
