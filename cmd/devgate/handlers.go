package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/ratelimit"
	"github.com/chyioishi/devgate/internal/requesttimeout"
	"github.com/chyioishi/devgate/internal/router"
)

func handlersFromRoutes(
	routes []router.Route,
	transport http.RoundTripper,
	circuitFailureThreshold int,
	circuitOpenTimeout time.Duration,
	circuitBreakerMetrics *metrics.CircuitBreaker,
	rateLimiterMetrics *metrics.RateLimiter,
	logger *slog.Logger,
) (map[string]http.Handler, error) {
	handlers := make(map[string]http.Handler, len(routes))

	for _, route := range routes {
		switch route.Protocol {
		case router.ProtocolHTTP:
			circuitBreakerRouteMetrics := circuitBreakerMetrics.ForRoute(route.Name)
			circuitBreakerTransport, err := proxy.NewCircuitBreakerTransport(
				transport,
				circuitFailureThreshold,
				circuitOpenTimeout,
				circuitBreakerRouteMetrics,
			)
			if err != nil {
				return nil, fmt.Errorf("create circuit breaker transport for route %q: %w", route.Name, err)
			}

			var routeHandler http.Handler = proxy.New(route.UpstreamURL, circuitBreakerTransport, logger)

			if route.PathPrefix != "/" && route.StripPathPrefix {
				routeHandler = http.StripPrefix(route.PathPrefix, routeHandler)
			}

			if route.RequestTimeout != 0 {
				routeHandler, err = requesttimeout.New(routeHandler, route.RequestTimeout)
				if err != nil {
					return nil, fmt.Errorf(
						"create request timeout handler for route %q: %w",
						route.Name,
						err,
					)
				}
			}

			if route.RateLimit != nil {
				limiter, err := ratelimit.NewLocal(
					route.RateLimit.RequestsPerSecond,
					route.RateLimit.Burst,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"create rate limiter for route %q: %w", route.Name, err,
					)
				}
				limiterMetrics := rateLimiterMetrics.ForRoute(route.Name)
				routeHandler = ratelimit.Middleware(routeHandler, limiter, limiterMetrics)
			}
			handlers[route.Name] = routeHandler

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
