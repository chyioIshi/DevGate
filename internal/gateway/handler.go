package gateway

import (
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
)

type Handler struct {
	routeRouter   *router.Router
	routeHandlers map[string]http.Handler
	logger        *slog.Logger
}

func New(routeRouter *router.Router, routeHandlers map[string]http.Handler, logger *slog.Logger) *Handler {
	routeHandlersCopy := maps.Clone(routeHandlers)
	return &Handler{
		routeRouter:   routeRouter,
		routeHandlers: routeHandlersCopy,
		logger:        logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	rw := newResponseWriter(w)
	requestID, _ := requestid.FromContext(r.Context())
	var routeName string
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
