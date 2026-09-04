package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/router"
)

func handlersFromRoutes(
	routes []router.Route,
	transport http.RoundTripper,
	circuitFailureThreshold int,
	circuitOpenTimeout time.Duration,
	logger *slog.Logger,
) (map[string]http.Handler, error) {
	handlers := make(map[string]http.Handler, len(routes))

	for _, route := range routes {
		switch route.Protocol {
		case router.ProtocolHTTP:
			circuitBreakerTransport, err := proxy.NewCircuitBreakerTransport(
				transport,
				circuitFailureThreshold,
				circuitOpenTimeout,
			)
			if err != nil {
				return nil, fmt.Errorf("create circuit breaker transport for route %q: %w", route.Name, err)
			}
			handlers[route.Name] = proxy.New(route.UpstreamURL, circuitBreakerTransport, logger)
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
