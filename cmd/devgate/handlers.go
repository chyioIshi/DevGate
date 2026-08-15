package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/router"
)

func handlersFromRoutes(routes []router.Route, logger *slog.Logger) (map[string]http.Handler, error) {
	handlers := make(map[string]http.Handler, len(routes))

	for _, route := range routes {
		switch route.Protocol {
		case router.ProtocolHTTP:
			handlers[route.Name] = proxy.New(route.UpstreamURL, logger)
		case router.ProtocolGRPC:
			return nil, fmt.Errorf(
				"create handler for route %q: gRPC protocol is not supported yet",
				route.Name,
		)
		default:
			return nil, fmt.Errorf(
				"create handler for route %q: unsupported protocol %q",
				route.Name,
				route.Protocol,
			)
		}
	}

	return handlers, nil
}
