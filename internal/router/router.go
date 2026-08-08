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
		if strings.TrimSpace(route.Name) == "" {
			return nil, fmt.Errorf("create router: route name must not be empty string")
		}
		if route.Protocol != ProtocolHTTP && route.Protocol != ProtocolGRPC {
			return nil, fmt.Errorf("create router: route %q: unsupported protocol %q", route.Name, route.Protocol)
		}
		if !strings.HasPrefix(route.PathPrefix, "/") {
			return nil, fmt.Errorf("create router: route %q: path prefix %q must start with '/'", route.Name, route.PathPrefix)
		}
		if strings.HasSuffix(route.PathPrefix, "/") && route.PathPrefix != "/" {
			return nil, fmt.Errorf("create router: route %q: path prefix %q must not end with '/' unless it is '/'", route.Name, route.PathPrefix)
		}
		if route.UpstreamURL == nil {
			return nil, fmt.Errorf("create router: route %q: upstream URL must not be nil", route.Name)
		}
		if route.UpstreamURL.Scheme != "http" && route.UpstreamURL.Scheme != "https" {
			return nil, fmt.Errorf("create router: route %q: upstream URL scheme %q must be either 'http' or 'https'", route.Name, route.UpstreamURL.Scheme)
		}
		if strings.TrimSpace(route.UpstreamURL.Host) == "" {
			return nil, fmt.Errorf("create router: route %q: upstream URL host must not be empty", route.Name)
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
