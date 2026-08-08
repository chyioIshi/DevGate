package main

import (
	"fmt"
	"net/url"

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
		route := router.Route{
			Name:        routeConfig.Name,
			Protocol:    router.Protocol(routeConfig.Protocol),
			PathPrefix:  routeConfig.PathPrefix,
			UpstreamURL: upstreamURL,
		}
		routes = append(routes, route)
	}

	return routes, nil
}
