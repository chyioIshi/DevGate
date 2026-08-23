package gateway

import (
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
)

type Handler struct {
	routeRouter   *router.Router
	routeHandlers map[string]http.Handler
	logger        *slog.Logger
	httpMetrics   *metrics.HTTP
}

func New(
	routeRouter *router.Router,
	routeHandlers map[string]http.Handler,
	logger *slog.Logger,
	httpMetrics *metrics.HTTP,
) *Handler {
	routeHandlersCopy := maps.Clone(routeHandlers)
	return &Handler{
		routeRouter:   routeRouter,
		routeHandlers: routeHandlersCopy,
		logger:        logger,
		httpMetrics:   httpMetrics,
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
	defer recoverPanic(rw, r, h.logger)

	route, ok := h.routeRouter.Match(r.URL.Path)
	if !ok {
		http.NotFound(rw, r)
		return
	}
	routeName = route.Name

	routeHandler, exists := h.routeHandlers[route.Name]

	if !exists {
		http.Error(rw, "route handler not found", http.StatusInternalServerError)
		return
	}

	routeHandler.ServeHTTP(rw, r)
}
