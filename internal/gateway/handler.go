package gateway

import (
	"maps"
	"net/http"

	"github.com/chyioishi/devgate/internal/router"
)

type Handler struct {
	routeRouter   *router.Router
	routeHandlers map[string]http.Handler
}

func New(routeRouter *router.Router, routeHandlers map[string]http.Handler) *Handler {
	routeHandlersCopy := maps.Clone(routeHandlers)
	return &Handler{
		routeRouter:   routeRouter,
		routeHandlers: routeHandlersCopy,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := h.routeRouter.Match(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	routeHandler, exists := h.routeHandlers[route.Name]

	if !exists {
		http.Error(w, "route handler not found", http.StatusInternalServerError)
		return
	}

	routeHandler.ServeHTTP(w, r)
}
