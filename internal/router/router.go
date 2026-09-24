package router

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
)

type Router struct {
	routes []Route
}

func New(routes []Route) (*Router, error) {
	uniqueNames := make(map[string]struct{}, len(routes))
	routesByPathMatcher := make(map[pathMatchKey][]Route, len(routes))
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
		routePathMatchKey := pathMatchKeyForRoute(route)
		existingRoutes, exists := routesByPathMatcher[routePathMatchKey]
		if exists {
			for _, existingRoute := range existingRoutes {
				if route.Priority != existingRoute.Priority {
					continue
				}

				if methodSetsOverlap(
					route.Methods,
					existingRoute.Methods,
				) && hostSetsOverlap(
					route.Hosts,
					existingRoute.Hosts,
				) && headerMatchSetsOverlap(
					route.HeaderMatches,
					existingRoute.HeaderMatches,
				) {
					return nil, fmt.Errorf(
						"create router: routes %q and %q have conflicting matchers for path %q at priority %d",
						route.Name,
						existingRoute.Name,
						routePathMatchKey.path,
						route.Priority,
					)
				}
			}
		}
		routesByPathMatcher[routePathMatchKey] = append(existingRoutes, route)
		uniqueNames[route.Name] = struct{}{}
	}
	routerRoutes := slices.Clone(routes)
	return &Router{routes: routerRoutes}, nil
}

func (r *Router) Match(method, host, path string, headers http.Header) (Route, bool) {
	if !strings.HasPrefix(path, "/") {
		return Route{}, false
	}

	host = normalizeHost(host)

	bestMatch := Route{}
	bestMatchPathLength := -1
	for _, route := range r.routes {
		pathMatches := routeMatchesPath(route, path)
		methodMatches := routeMatchesMethod(route, method)
		hostMatches := routeMatchesHost(route, host)
		headersMatch := routeMatchesHeaders(route, headers)
		matches := pathMatches && methodMatches && hostMatches && headersMatch
		if !matches {
			continue
		}
		candidateIsExact := route.PathExact != ""
		candidatePathLength := len(route.PathPrefix)
		if candidateIsExact {
			candidatePathLength = len(route.PathExact)
		}

		bestIsExact := bestMatch.PathExact != ""

		var candidateIsBetter bool
		switch {
		case candidatePathLength > bestMatchPathLength:
			candidateIsBetter = true
		case candidatePathLength < bestMatchPathLength:
			candidateIsBetter = false
		case candidateIsExact != bestIsExact:
			candidateIsBetter = candidateIsExact
		default:
			candidateIsBetter = route.Priority > bestMatch.Priority
		}

		if candidateIsBetter {
			bestMatch = route
			bestMatchPathLength = candidatePathLength
		}
	}
	if bestMatchPathLength == -1 {
		return Route{}, false
	}
	return bestMatch, true
}

// AllowedMethods returns the sorted, unique methods configured for routes that
// match the host, path, and request headers. It returns nil when the path is
// invalid, the host, path, or headers are unmatched, or a matching route allows
// every method.
func (r *Router) AllowedMethods(host, path string, headers http.Header) []string {
	if !strings.HasPrefix(path, "/") {
		return nil
	}
	host = normalizeHost(host)
	allowedMethods := make([]string, 0)
	for _, route := range r.routes {
		if routeMatchesPath(route, path) && routeMatchesHost(route, host) && routeMatchesHeaders(route, headers) {
			if len(route.Methods) == 0 {
				return nil
			}
			allowedMethods = append(allowedMethods, route.Methods...)
		}
	}
	slices.Sort(allowedMethods)
	return slices.Compact(allowedMethods)
}

type pathMatchKey struct {
	path  string
	exact bool
}

func pathMatchKeyForRoute(route Route) pathMatchKey {
	if route.PathExact != "" {
		return pathMatchKey{
			path:  route.PathExact,
			exact: true,
		}
	}
	return pathMatchKey{
		path:  route.PathPrefix,
		exact: false,
	}
}

func routeMatchesPath(route Route, path string) bool {
	if route.PathExact != "" {
		return path == route.PathExact
	}
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

func routeMatchesHeaders(route Route, headers http.Header) bool {
	if len(route.HeaderMatches) == 0 {
		return true
	}
	for _, headerMatch := range route.HeaderMatches {
		headerValues := headers.Values(headerMatch.Name)
		if len(headerValues) != 1 {
			return false
		}
		if headerValues[0] != headerMatch.Exact {
			return false
		}
	}
	return true
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

func headerMatchSetsOverlap(current, existing []HeaderMatch) bool {
	if len(current) == 0 || len(existing) == 0 {
		return true
	}
	set := make(map[string]string, len(current))
	for _, currentHeaderMatch := range current {
		set[strings.ToLower(currentHeaderMatch.Name)] = currentHeaderMatch.Exact
	}
	for _, existingHeaderMatch := range existing {
		if currentHeaderMatch, exists := set[strings.ToLower(existingHeaderMatch.Name)]; exists {
			if currentHeaderMatch != existingHeaderMatch.Exact {
				return false
			}
		}
	}
	return true
}

func normalizeHost(host string) string {
	host = strings.ToLower(host)
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return hostname
}
