package gateway

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/chyioishi/devgate/internal/requestid"
)

func recoverPanic(w *responseWriter, r *http.Request, logger *slog.Logger) {
	rec := recover()
	if rec == nil {
		return
	}
	if rec == http.ErrAbortHandler { //nolint:errorlint // Only the exact net/http sentinel must bypass recovery.
		panic(rec)
	}
	requestID, _ := requestid.FromContext(r.Context())
	logger.ErrorContext(r.Context(),
		"panic recovered",
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", requestID,
		"panic", rec,
		"stack", string(debug.Stack()),
	)
	if !w.wroteHeader {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	} else {
		panic(http.ErrAbortHandler)
	}
}
