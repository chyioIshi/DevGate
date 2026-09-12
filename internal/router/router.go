package router

import (
	"fmt"
	"slices"
	"strings"
)

type Router struct {
	routes []Route
}

func New(routes []Route) (*Router, error) {
	uniqueNames := make(map[string]struct{}, len(routes))
	uniquePathPrefixes := make(map[string]struct{}, len(routes))
	if len(routes) == 0 {
		return nil, fmt.Errorf("create router: routes length must be greater than 0")
	}
	for _, route := range routes {
		if err := route.validate(); err != nil {
			return nil, fmt.Errorf("create router: route %q: %w", route.Name, err)
		}
		if _, exists := uniqueNames[route.Name]; exists {
			return nil, fmt.Errorf("create router: duplicate route name '%s'", route.Name)
		}
		if _, exists := uniquePathPrefixes[route.PathPrefix]; exists {
			return nil, fmt.Errorf("create router: duplicate route path prefix '%s'", route.PathPrefix)
		}
		uniquePathPrefixes[route.PathPrefix] = struct{}{}
		uniqueNames[route.Name] = struct{}{}
	}
	routerRoutes := slices.Clone(routes)
	return &Router{routes: routerRoutes}, nil
}

func (r *Router) Match(path string) (Route, bool) {
	if !strings.HasPrefix(path, "/") {
		return Route{}, false
	}

	bestMatch := Route{}
	bestMatchLength := -1
	for _, route := range r.routes {
		matches := route.PathPrefix == "/" || path == route.PathPrefix || strings.HasPrefix(path, route.PathPrefix+"/")
		if !matches {
			continue
		}
		if len(route.PathPrefix) > bestMatchLength {
			bestMatch = route
			bestMatchLength = len(route.PathPrefix)
		}
	}
	if bestMatchLength == -1 {
		return Route{}, false
	}
	return bestMatch, true
}
