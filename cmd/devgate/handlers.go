package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
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

type routeRuntime struct {
	handlers      map[string]http.Handler
	healthRunners []healthRunner
}

func (r *routeRuntime) startHealthRunners(ctx context.Context) func() {
	healthRunCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup

	for _, runner := range r.healthRunners {
		wg.Go(func() {
			runner.Run(healthRunCtx)
		})
	}

	return func() {
		cancel()
		wg.Wait()
	}
}

type replaceableTargetPicker interface {
	proxy.TargetPicker
	Replace([]url.URL) error
}

type healthRunner interface {
	Run(context.Context)
}

func routeRuntimeFromRoutes(
	routes []router.Route,
	transport http.RoundTripper,
	healthCheckClient *http.Client,
	circuitFailureThreshold int,
	circuitOpenTimeout time.Duration,
	circuitBreakerMetrics *metrics.CircuitBreaker,
	rateLimiterMetrics *metrics.RateLimiter,
	activeHealthCheckMetrics *metrics.ActiveHealthCheck,
	passiveHealthCheckMetrics *metrics.PassiveHealth,
	trustedCIDRs []netip.Prefix,
	logger *slog.Logger,
) (*routeRuntime, error) {
	runtime := &routeRuntime{
		handlers: make(map[string]http.Handler, len(routes)),
	}

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
				var requestHeaderTransform proxy.RequestHeaderTransform
				if route.RequestHeaders != nil {
					requestHeaderTransform = route.RequestHeaders.Apply
				}

				var targetPicker replaceableTargetPicker
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
				case router.LoadBalancingPolicyLeastRequests:
					targetPicker, err = upstream.NewLeastRequests(route.Upstream.Endpoints)
					if err != nil {
						return nil, fmt.Errorf(
							"create least-requests target picker for route %q: %w",
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

				activeHealthTracker, err := newRouteActiveHealthTracker(
					route.Upstream,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"configure active health for route %q: %w",
						route.Name,
						err,
					)
				}
				passiveHealthTracker, err := newRoutePassiveHealthTracker(
					route.Upstream,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"configure passive health for route %q: %w",
						route.Name,
						err,
					)
				}

				selectedTargetPicker := proxy.TargetPicker(targetPicker)
				var healthAwarePicker *upstream.HealthAwarePicker
				if activeHealthTracker != nil || passiveHealthTracker != nil {
					healthAwarePicker, err = newRouteHealthAwarePicker(
						targetPicker,
						activeHealthTracker,
						passiveHealthTracker,
					)
					if err != nil {
						return nil, fmt.Errorf(
							"configure health-aware picker for route %q: %w",
							route.Name,
							err,
						)
					}
					selectedTargetPicker = healthAwarePicker
				}

				if activeHealthTracker != nil {
					activeHealthChecker, err := newRouteActiveHealthChecker(
						route.Name,
						route.Upstream,
						activeHealthTracker,
						healthAwarePicker,
						healthCheckClient,
						logger,
						activeHealthCheckMetrics,
					)
					if err != nil {
						return nil, fmt.Errorf(
							"configure active health checker for route %q: %w",
							route.Name,
							err,
						)
					}
					runtime.healthRunners = append(runtime.healthRunners, activeHealthChecker)
				}

				routeTransport := transport

				if passiveHealthTracker != nil {
					passiveHealthCheckRouteMetrics := passiveHealthCheckMetrics.ForRoute(
						route.Name,
						route.Upstream.Endpoints,
					)
					routePassiveHealthObserver, err := newRoutePassiveHealthObserver(
						route.Name,
						passiveHealthTracker,
						healthAwarePicker,
						logger,
						passiveHealthCheckRouteMetrics,
					)
					if err != nil {
						return nil, fmt.Errorf(
							"create passive health observer for route %q: %w",
							route.Name,
							err,
						)
					}
					routePassiveHealthTransport, err := upstream.NewPassiveHealthTransport(
						routeTransport,
						passiveHealthTracker,
						routePassiveHealthObserver,
					)
					if err != nil {
						return nil, fmt.Errorf(
							"create passive health transport for route %q: %w",
							route.Name,
							err,
						)
					}
					runtime.healthRunners = append(runtime.healthRunners, routePassiveHealthObserver)
					routeTransport = routePassiveHealthTransport
				}

				circuitBreakerRouteMetrics := circuitBreakerMetrics.ForRoute(route.Name)
				circuitBreakerTransport, err := proxy.NewCircuitBreakerTransport(
					routeTransport,
					circuitFailureThreshold,
					circuitOpenTimeout,
					circuitBreakerRouteMetrics,
				)
				if err != nil {
					return nil, fmt.Errorf("create circuit breaker transport for route %q: %w", route.Name, err)
				}

				routeHandler = proxy.New(
					selectedTargetPicker,
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
		runtime.handlers[route.Name] = routeHandler
	}

	return runtime, nil
}

func newRouteHealthAwarePicker(
	base replaceableTargetPicker,
	activeTracker *upstream.HealthTracker,
	passiveTracker *upstream.PassiveHealthTracker,
) (*upstream.HealthAwarePicker, error) {
	if activeTracker == nil && passiveTracker == nil {
		return nil, errors.New("both active and passive health trackers cannot be nil")
	}
	var healthSource *upstream.CompositeHealthSource
	var err error

	switch {
	case activeTracker == nil:
		healthSource, err = upstream.NewCompositeHealthSource(passiveTracker)
	case passiveTracker == nil:
		healthSource, err = upstream.NewCompositeHealthSource(activeTracker)
	default:
		healthSource, err = upstream.NewCompositeHealthSource(activeTracker, passiveTracker)
	}
	if err != nil {
		return nil, fmt.Errorf("create composite health source: %w", err)
	}

	healthAwarePicker, err := upstream.NewHealthAwarePicker(
		base,
		healthSource,
	)
	if err != nil {
		return nil, fmt.Errorf("create health-aware target picker: %w", err)
	}

	return healthAwarePicker, nil
}

func newRouteActiveHealthChecker(
	routeName string,
	routeUpstream *router.Upstream,
	tracker *upstream.HealthTracker,
	picker *upstream.HealthAwarePicker,
	client *http.Client,
	logger *slog.Logger,
	activeHealthCheckMetrics *metrics.ActiveHealthCheck,
) (*upstream.HealthChecker, error) {
	healthCheckObserver := &routeHealthCheckObserver{
		routeName: routeName,
		picker:    picker,
		logger:    logger,
		metricsObserver: activeHealthCheckMetrics.ForRoute(
			routeName,
			routeUpstream.Endpoints,
		),
	}
	routeHealthChecker, err := upstream.NewHealthChecker(
		client,
		tracker,
		routeUpstream.ActiveHealthCheck.Interval,
		routeUpstream.ActiveHealthCheck.Timeout,
		routeUpstream.ActiveHealthCheck.Path,
		routeUpstream.ActiveHealthCheck.MaxConcurrentProbes,
		healthCheckObserver,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create active health checker: %w",
			err,
		)
	}
	return routeHealthChecker, nil
}

func newRouteActiveHealthTracker(
	routeUpstream *router.Upstream,
) (*upstream.HealthTracker, error) {
	if routeUpstream == nil {
		return nil, errors.New("route upstream was not provided")
	}
	if routeUpstream.ActiveHealthCheck == nil {
		return nil, nil
	}
	routeActiveHealthTracker, err := upstream.NewHealthTracker(
		routeUpstream.Endpoints,
		routeUpstream.ActiveHealthCheck.HealthyThreshold,
		routeUpstream.ActiveHealthCheck.UnhealthyThreshold,
	)
	if err != nil {
		return nil, fmt.Errorf("create active health tracker: %w", err)
	}
	return routeActiveHealthTracker, nil
}

func newRoutePassiveHealthTracker(
	routeUpstream *router.Upstream,
) (*upstream.PassiveHealthTracker, error) {
	if routeUpstream == nil {
		return nil, errors.New("route upstream was not provided")
	}
	if routeUpstream.PassiveHealthCheck == nil {
		return nil, nil
	}

	routePassiveHealthTracker, err := upstream.NewPassiveHealthTracker(
		routeUpstream.Endpoints,
		routeUpstream.PassiveHealthCheck.ConsecutiveFailures,
		routeUpstream.PassiveHealthCheck.EjectionDuration,
	)
	if err != nil {
		return nil, fmt.Errorf("create passive health tracker: %w", err)
	}
	return routePassiveHealthTracker, nil
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
