package router

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"
)

func BenchmarkRouterMatch(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_routes", count), func(b *testing.B) {
			routes := benchmarkRoutes(count)
			routeRouter, err := New(routes)
			if err != nil {
				b.Fatalf("failed to create router: %v", err)
			}
			targetPath := fmt.Sprintf("/routes/%d", count-1)
			route, ok := routeRouter.Match(
				http.MethodGet,
				"api.example.com",
				targetPath,
				nil,
			)
			if !ok {
				b.Fatalf("failed to match route for path: %s", targetPath)
			}
			if route.Name != fmt.Sprintf("route-%d", count-1) {
				b.Fatalf("matched route has unexpected name: %s", route.Name)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = routeRouter.Match(
					http.MethodGet,
					"api.example.com",
					targetPath,
					nil,
				)
			}
		})
	}
}

func BenchmarkRouterMethodNotAllowed(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_routes", count), func(b *testing.B) {
			routes := benchmarkRoutes(count)
			for i := range routes {
				routes[i].Methods = []string{http.MethodGet}
			}
			routeRouter, err := New(routes)
			if err != nil {
				b.Fatalf("failed to create router: %v", err)
			}
			targetPath := fmt.Sprintf("/routes/%d", count-1)
			_, ok := routeRouter.Match(
				http.MethodPost,
				"api.example.com",
				targetPath,
				nil,
			)
			if ok {
				b.Fatalf("match route should not be allowed for path: %s", targetPath)
			}
			allowedMethods := routeRouter.AllowedMethods(
				"api.example.com",
				targetPath,
				nil,
			)
			if !slices.Equal(allowedMethods, []string{http.MethodGet}) {
				b.Fatalf("allowed methods should include only GET for path: %s", targetPath)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, ok := routeRouter.Match(
					http.MethodPost,
					"api.example.com",
					targetPath,
					nil,
				)
				if !ok {
					_ = routeRouter.AllowedMethods(
						"api.example.com",
						targetPath,
						nil,
					)
				}
			}
		})
	}
}

func BenchmarkRouterMatchParallel(b *testing.B) {
	const routesCount = 1000
	targetIndex := routesCount - 1
	routes := benchmarkRoutes(routesCount)
	routeRouter, err := New(routes)
	if err != nil {
		b.Fatalf("failed to create router: %v", err)
	}
	targetPath := fmt.Sprintf("/routes/%d", targetIndex)
	route, ok := routeRouter.Match(
		http.MethodGet,
		"api.example.com",
		targetPath,
		nil,
	)
	if !ok {
		b.Fatalf("failed to match route for path: %s", targetPath)
	}
	if route.Name != fmt.Sprintf("route-%d", targetIndex) {
		b.Fatalf("matched route has unexpected name: %s", route.Name)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = routeRouter.Match(
				http.MethodGet,
				"api.example.com",
				targetPath,
				nil,
			)
		}
	})
}

func benchmarkRoutes(routesCount int) []Route {
	routes := make([]Route, 0, routesCount)
	upstreamURL := &url.URL{Scheme: "http", Host: "api.example.com"}
	for i := 0; i < routesCount; i++ {
		routes = append(routes, Route{
			Name:        fmt.Sprintf("route-%d", i),
			Protocol:    ProtocolHTTP,
			PathExact:   fmt.Sprintf("/routes/%d", i),
			UpstreamURL: upstreamURL,
		})
	}
	return routes
}
