package main

import (
	"fmt"
	"maps"
	"net/url"
	"slices"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
)

func routesFromConfig(routeConfigs []config.RouteConfig) ([]router.Route, error) {
	routes := make([]router.Route, 0, len(routeConfigs))
	for _, routeConfig := range routeConfigs {
		routeUpstream, err := upstreamFromConfig(routeConfig)
		if err != nil {
			return nil, err
		}
		var directResponse *router.DirectResponse
		if routeConfig.DirectResponse != nil {
			directResponse = &router.DirectResponse{
				StatusCode: routeConfig.DirectResponse.StatusCode,
				Body:       routeConfig.DirectResponse.Body,
			}
		}
		var redirect *router.Redirect
		if routeConfig.Redirect != nil {
			redirect = &router.Redirect{
				StatusCode: routeConfig.Redirect.StatusCode,
				Location:   routeConfig.Redirect.Location,
			}
		}
		requestHeaders := headerTransformPolicyFromConfig(
			routeConfig.RequestHeaders,
		)
		responseHeaders := headerTransformPolicyFromConfig(
			routeConfig.ResponseHeaders,
		)
		var rateLimit *router.RateLimitPolicy
		if routeConfig.RateLimit != nil {
			rateLimit = &router.RateLimitPolicy{
				RequestsPerSecond: routeConfig.RateLimit.RequestsPerSecond,
				Burst:             routeConfig.RateLimit.Burst,
			}
		}
		route := router.Route{
			Name:                routeConfig.Name,
			Protocol:            router.Protocol(routeConfig.Protocol),
			PathPrefix:          routeConfig.PathPrefix,
			PathExact:           routeConfig.PathExact,
			HeaderMatches:       headerMatchesFromConfig(routeConfig.HeaderMatches),
			Methods:             slices.Clone(routeConfig.Methods),
			Hosts:               slices.Clone(routeConfig.Hosts),
			Priority:            routeConfig.Priority,
			Upstream:            routeUpstream,
			DirectResponse:      directResponse,
			Redirect:            redirect,
			ErrorResponses:      errorResponsesFromConfig(routeConfig.ErrorResponses),
			RequestHeaders:      requestHeaders,
			ResponseHeaders:     responseHeaders,
			RateLimit:           rateLimit,
			StripPathPrefix:     routeConfig.StripPathPrefix,
			RequestTimeout:      routeConfig.RequestTimeout,
			MaxRequestBodyBytes: routeConfig.MaxRequestBodyBytes,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

func headerTransformPolicyFromConfig(headerConfig *config.HeaderTransformConfig) *router.HeaderTransformPolicy {
	if headerConfig == nil {
		return nil
	}
	return &router.HeaderTransformPolicy{
		Set:    maps.Clone(headerConfig.Set),
		Remove: slices.Clone(headerConfig.Remove),
	}
}

func headerMatchesFromConfig(configHeaderMatches []config.HeaderMatchConfig) []router.HeaderMatch {
	if len(configHeaderMatches) == 0 {
		return nil
	}
	routeHeaderMatches := make(
		[]router.HeaderMatch,
		0,
		len(configHeaderMatches),
	)
	for _, header := range configHeaderMatches {
		routeHeaderMatch := router.HeaderMatch{
			Name:  header.Name,
			Exact: header.Exact,
		}
		routeHeaderMatches = append(routeHeaderMatches, routeHeaderMatch)
	}

	return routeHeaderMatches
}

func upstreamFromConfig(routeConfig config.RouteConfig) (*router.Upstream, error) {
	if routeConfig.Upstream != nil && routeConfig.UpstreamURL != "" {
		return nil, fmt.Errorf("both upstream and upstream URL are configured for route %q", routeConfig.Name)
	}
	if routeConfig.Upstream == nil && routeConfig.UpstreamURL == "" {
		return nil, nil
	}

	if routeConfig.UpstreamURL != "" {
		upstreamURL, err := url.Parse(routeConfig.UpstreamURL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream URL for route %q: %w", routeConfig.Name, err)
		}
		routeUpstream := &router.Upstream{
			LoadBalancing: router.LoadBalancingPolicyRoundRobin,
			Endpoints:     []url.URL{*upstreamURL},
		}
		return routeUpstream, nil
	}
	if routeConfig.Upstream != nil {
		if routeConfig.Upstream.Discovery == nil {
			return nil, fmt.Errorf("no discovery configuration for upstream of route %q", routeConfig.Name)
		}
		if routeConfig.Upstream.Discovery.Static == nil {
			return nil, fmt.Errorf("no static discovery configuration for upstream of route %q", routeConfig.Name)
		}
		var loadBalancing router.LoadBalancingPolicy
		switch routeConfig.Upstream.LoadBalancing {
		case "", config.LoadBalancingPolicyRoundRobin:
			loadBalancing = router.LoadBalancingPolicyRoundRobin
		case config.LoadBalancingPolicyRandom:
			loadBalancing = router.LoadBalancingPolicyRandom
		default:
			return nil, fmt.Errorf(
				"invalid load balancing policy %q for route %q",
				routeConfig.Upstream.LoadBalancing,
				routeConfig.Name,
			)
		}
		routeUpstream := &router.Upstream{
			LoadBalancing: loadBalancing,
			Endpoints:     make([]url.URL, len(routeConfig.Upstream.Discovery.Static.Endpoints)),
		}
		for i, endpointConfig := range routeConfig.Upstream.Discovery.Static.Endpoints {
			parsedEndpoint, err := url.Parse(endpointConfig.URL)
			if err != nil {
				return nil, fmt.Errorf(
					"parse upstream endpoint [%d] for route %q: %w",
					i,
					routeConfig.Name,
					err,
				)
			}
			routeUpstream.Endpoints[i] = *parsedEndpoint
		}
		return routeUpstream, nil
	}

	return nil, nil
}

func errorResponsesFromConfig(configErrorResponses map[int]config.ErrorResponseConfig) map[int]router.ErrorResponse {
	if len(configErrorResponses) == 0 {
		return nil
	}
	errorResponses := make(map[int]router.ErrorResponse, len(configErrorResponses))
	for statusCode, configErrorResponse := range configErrorResponses {
		errorResponses[statusCode] = router.ErrorResponse{
			Body:    configErrorResponse.Body,
			Headers: maps.Clone(configErrorResponse.Headers),
		}
	}
	return errorResponses
}
