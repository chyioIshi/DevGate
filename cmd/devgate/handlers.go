package main

import (
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/netip"
	"time"

	"github.com/chyioishi/devgate/internal/directresponse"
	"github.com/chyioishi/devgate/internal/errorresponse"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/ratelimit"
	"github.com/chyioishi/devgate/internal/redirect"
	"github.com/chyioishi/devgate/internal/requestbodylimit"
	"github.com/chyioishi/devgate/internal/requesttimeout"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/chyioishi/devgate/internal/upstream"
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
		var actionCount int
		var err error

		if route.Upstream != nil {
			actionCount++
		}
		if route.DirectResponse != nil {
			actionCount++
		}
		if route.Redirect != nil {
			actionCount++
		}

		switch {
		case actionCount == 0:
			return nil, fmt.Errorf(
				"create handler for route %q: no action configured for route",
				route.Name,
			)
		case actionCount > 1:
			return nil, fmt.Errorf(
				"create handler for route %q: multiple actions configured for route",
				route.Name,
			)
		}

		errorResponses := errorResponsesFromRoute(
			route.ErrorResponses,
		)
		routeErrorResponder, err := errorresponse.New(errorResponses)
		if err != nil {
			return nil, fmt.Errorf("create error responder for route %q: %w", route.Name, err)
		}

		switch {
		case route.Upstream != nil:
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

				var targetPicker proxy.TargetPicker
				switch route.Upstream.LoadBalancing {
				case router.LoadBalancingPolicyRoundRobin:
					targetPicker, err = upstream.NewRoundRobin(route.Upstream.Endpoints)
					if err != nil {
						return nil, fmt.Errorf(
							"create round-robin target picker for route %q: %w",
							route.Name,
							err,
						)
					}
				case router.LoadBalancingPolicyRandom:
					targetPicker, err = upstream.NewRandom(route.Upstream.Endpoints)
					if err != nil {
						return nil, fmt.Errorf(
							"create random target picker for route %q: %w",
							route.Name,
							err,
						)
					}
				default:
					return nil, fmt.Errorf(
						"create handler for route %q: unsupported load balancing policy %q",
						route.Name,
						route.Upstream.LoadBalancing,
					)
				}
				routeHandler = proxy.New(
					targetPicker,
					circuitBreakerTransport,
					requestHeaderTransform,
					responseHeaderTransform,
					trustedCIDRs,
					routeErrorResponder.Write,
					logger,
				)

				if route.PathPrefix != "/" && route.StripPathPrefix {
					routeHandler = http.StripPrefix(route.PathPrefix, routeHandler)
				}

				if route.MaxRequestBodyBytes != 0 {
					routeHandler, err = requestbodylimit.New(
						routeHandler,
						route.MaxRequestBodyBytes,
						routeErrorResponder.Write,
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
		case route.Redirect != nil:
			routeHandler, err = redirect.New(
				route.Redirect.StatusCode,
				route.Redirect.Location,
				responseHeaderTransform,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"create redirect handler for route %q: %w",
					route.Name,
					err,
				)
			}
		default:
			return nil, fmt.Errorf(
				"create handler for route %q: no action configured for route",
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
			routeHandler = ratelimit.Middleware(
				routeHandler,
				limiter, limiterMetrics,
				routeErrorResponder.Write,
			)
		}
		handlers[route.Name] = routeHandler
	}

	return handlers, nil
}

func errorResponsesFromRoute(routeErrorResponses map[int]router.ErrorResponse) map[int]errorresponse.Response {
	errorResponses := make(map[int]errorresponse.Response, len(routeErrorResponses))

	for statusCode, routeErrorResponse := range routeErrorResponses {
		errorResponses[statusCode] = errorresponse.Response{
			Body:    routeErrorResponse.Body,
			Headers: maps.Clone(routeErrorResponse.Headers),
		}
	}
	return errorResponses
}
