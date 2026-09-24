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
		upstreamURL, err := url.Parse(routeConfig.UpstreamURL)
		if err != nil {
			return nil, fmt.Errorf("parse upstream URL for route %q: %w", routeConfig.Name, err)
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
			UpstreamURL:         upstreamURL,
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
