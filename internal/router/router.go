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
	methodsByPathPrefix := make(map[string][]string, len(routes))
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
		existingMethods, exists := methodsByPathPrefix[route.PathPrefix]
		if exists && methodSetsOverlap(route.Methods, existingMethods) {
			return nil, fmt.Errorf(
				"create router: conflicting methods for route path prefix '%s'",
				route.PathPrefix,
			)
		}
		methodsByPathPrefix[route.PathPrefix] = append(slices.Clone(route.Methods), existingMethods...)
		uniqueNames[route.Name] = struct{}{}
	}
	routerRoutes := slices.Clone(routes)
	return &Router{routes: routerRoutes}, nil
}

func (r *Router) Match(method, path string) (Route, bool) {
	if !strings.HasPrefix(path, "/") {
		return Route{}, false
	}

	bestMatch := Route{}
	bestMatchLength := -1
	for _, route := range r.routes {
		pathMatches := route.PathPrefix == "/" ||
			path == route.PathPrefix ||
			strings.HasPrefix(path, route.PathPrefix+"/")
		methodMatches := routeMatchesMethod(route, method)
		matches := pathMatches && methodMatches
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

func routeMatchesMethod(route Route, method string) bool {
	if len(route.Methods) == 0 {
		return true
	}
	for _, m := range route.Methods {
		if m == method {
			return true
		}
	}
	return false
}

func methodSetsOverlap(current, existing []string) bool {
	if len(current) == 0 || len(existing) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(current))
	for _, m := range current {
		set[m] = struct{}{}
	}
	for _, m := range existing {
		if _, exists := set[m]; exists {
			return true
		}
	}
	return false
}
