package gateway

import (
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
)

// ErrorResponder writes a gateway-generated HTTP error response.
type ErrorResponder func(http.ResponseWriter, *http.Request, int)

type Handler struct {
	routeRouter    *router.Router
	routeHandlers  map[string]http.Handler
	errorResponder ErrorResponder
	logger         *slog.Logger
	httpMetrics    *metrics.HTTP
}

func New(
	routeRouter *router.Router,
	routeHandlers map[string]http.Handler,
	errorResponder ErrorResponder,
	logger *slog.Logger,
	httpMetrics *metrics.HTTP,
) *Handler {
	routeHandlersCopy := maps.Clone(routeHandlers)
	return &Handler{
		routeRouter:    routeRouter,
		routeHandlers:  routeHandlersCopy,
		errorResponder: errorResponder,
		logger:         logger,
		httpMetrics:    httpMetrics,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	h.httpMetrics.RequestStarted()
	rw := newResponseWriter(w)
	requestID, _ := requestid.FromContext(r.Context())
	routeName := "unmatched"
	defer func() {
		h.httpMetrics.RequestFinished(
			routeName,
			rw.statusCode,
			time.Since(startedAt),
		)
	}()
	defer func() {
		h.logger.InfoContext(r.Context(),
			"request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"route", routeName,
			"request_id", requestID,
			"status", rw.statusCode,
			"bytes", rw.bytesWritten,
			"duration_ms", time.Since(startedAt).Seconds()*1000,
		)
	}()
	defer recoverPanic(rw, r, h.writeError, h.logger)

	route, ok := h.routeRouter.Match(r.Method, r.Host, r.URL.Path, r.Header)
	if !ok {
		allowedMethods := h.routeRouter.AllowedMethods(r.Host, r.URL.Path, r.Header)
		if len(allowedMethods) != 0 {
			w.Header().Set(
				"Allow",
				strings.Join(allowedMethods, ", "),
			)
			h.writeError(rw, r, http.StatusMethodNotAllowed)
			return
		}
		h.writeError(rw, r, http.StatusNotFound)
		return
	}
	routeName = route.Name

	routeHandler, exists := h.routeHandlers[route.Name]

	if !exists {
		h.writeError(rw, r, http.StatusInternalServerError)
		return
	}

	routeHandler.ServeHTTP(rw, r)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, statusCode int) {
	if h.errorResponder != nil {
		h.errorResponder(w, r, statusCode)
	} else {
		http.Error(
			w,
			http.StatusText(statusCode),
			statusCode,
		)
	}
}
