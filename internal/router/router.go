package router

import (
	"fmt"
	"net"
	"slices"
	"strings"
)

type Router struct {
	routes []Route
}

func New(routes []Route) (*Router, error) {
	uniqueNames := make(map[string]struct{}, len(routes))
	routesByPathPrefix := make(map[string][]Route, len(routes))
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
		existingRoutes, exists := routesByPathPrefix[route.PathPrefix]
		if exists {
			for _, existingRoute := range existingRoutes {
				if methodSetsOverlap(
					route.Methods,
					existingRoute.Methods,
				) && hostSetsOverlap(
					route.Hosts,
					existingRoute.Hosts,
				) {
					return nil, fmt.Errorf(
						"create router: routes %q and %q have conflicting matchers for path prefix %q",
						route.Name,
						existingRoute.Name,
						route.PathPrefix,
					)
				}
			}
		}
		routesByPathPrefix[route.PathPrefix] = append(existingRoutes, route)
		uniqueNames[route.Name] = struct{}{}
	}
	routerRoutes := slices.Clone(routes)
	return &Router{routes: routerRoutes}, nil
}

func (r *Router) Match(method, host, path string) (Route, bool) {
	if !strings.HasPrefix(path, "/") {
		return Route{}, false
	}

	host = normalizeHost(host)

	bestMatch := Route{}
	bestMatchLength := -1
	for _, route := range r.routes {
		pathMatches := routeMatchesPath(route, path)
		methodMatches := routeMatchesMethod(route, method)
		hostMatches := routeMatchesHost(route, host)
		matches := pathMatches && methodMatches && hostMatches
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

// AllowedMethods returns the sorted, unique methods configured for routes that
// match host and path. It returns nil when the path is invalid, the host or path
// is unmatched, or a matching route allows every method.
func (r *Router) AllowedMethods(host, path string) []string {
	if !strings.HasPrefix(path, "/") {
		return nil
	}
	host = normalizeHost(host)
	allowedMethods := make([]string, 0)
	for _, route := range r.routes {
		if routeMatchesPath(route, path) && routeMatchesHost(route, host) {
			if len(route.Methods) == 0 {
				return nil
			}
			allowedMethods = append(allowedMethods, route.Methods...)
		}
	}
	slices.Sort(allowedMethods)
	return slices.Compact(allowedMethods)
}

func routeMatchesPath(route Route, path string) bool {
	return route.PathPrefix == "/" ||
		path == route.PathPrefix ||
		strings.HasPrefix(path, route.PathPrefix+"/")
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

func routeMatchesHost(route Route, host string) bool {
	if len(route.Hosts) == 0 {
		return true
	}
	for _, h := range route.Hosts {
		if h == host {
			return true
		}
	}
	return false
}

func hostSetsOverlap(current, existing []string) bool {
	if len(current) == 0 || len(existing) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(current))
	for _, h := range current {
		set[h] = struct{}{}
	}
	for _, h := range existing {
		if _, exists := set[h]; exists {
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

func normalizeHost(host string) string {
	host = strings.ToLower(host)
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return hostname
}
