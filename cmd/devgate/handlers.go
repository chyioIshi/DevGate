package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/chyioishi/devgate/internal/directresponse"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/ratelimit"
	"github.com/chyioishi/devgate/internal/requestbodylimit"
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
	trustedCIDRs []netip.Prefix,
	logger *slog.Logger,
) (map[string]http.Handler, error) {
	handlers := make(map[string]http.Handler, len(routes))

	for _, route := range routes {
		var responseHeaderTransform func(http.Header)
		if route.ResponseHeaders != nil {
			responseHeaderTransform = route.ResponseHeaders.Apply
		}

		var routeHandler http.Handler
		var err error

		if route.UpstreamURL != nil && route.DirectResponse != nil {
			return nil, fmt.Errorf(
				"create handler for route %q: upstream URL and direct response are mutually exclusive",
				route.Name,
			)
		}

		switch {
		case route.UpstreamURL != nil:
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

				var requestHeaderTransform proxy.RequestHeaderTransform
				if route.RequestHeaders != nil {
					requestHeaderTransform = route.RequestHeaders.Apply
				}

				routeHandler = proxy.New(
					route.UpstreamURL,
					circuitBreakerTransport,
					requestHeaderTransform,
					responseHeaderTransform,
					trustedCIDRs,
					logger,
				)

				if route.PathPrefix != "/" && route.StripPathPrefix {
					routeHandler = http.StripPrefix(route.PathPrefix, routeHandler)
				}

				if route.MaxRequestBodyBytes != 0 {
					routeHandler, err = requestbodylimit.New(
						routeHandler,
						route.MaxRequestBodyBytes,
					)
					if err != nil {
						return nil, fmt.Errorf(
							"create request body limit for route %q: %w",
							route.Name,
							err,
						)
					}
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
		case route.DirectResponse != nil:
			routeHandler, err = directresponse.New(
				route.DirectResponse.StatusCode,
				route.DirectResponse.Body,
				responseHeaderTransform,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"create direct response handler for route %q: %w",
					route.Name,
					err,
				)
			}
		default:
			return nil, fmt.Errorf(
				"create handler for route %q: no upstream URL or direct response specified",
				route.Name,
			)
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
	}

	return handlers, nil
}
