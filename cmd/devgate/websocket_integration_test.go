package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/websocket"
)

func TestGatewayProxiesWebSocketConnection(t *testing.T) {
	const message = "hello through DevGate"

	upstreamDone := make(chan error, 1)
	upstream := httptest.NewServer(websocket.Handler(func(connection *websocket.Conn) {
		var received string
		if err := websocket.Message.Receive(connection, &received); err != nil {
			upstreamDone <- err
			return
		}
		if received != message {
			upstreamDone <- errors.New("upstream received an unexpected WebSocket message")
			return
		}
		upstreamDone <- websocket.Message.Send(connection, received)
	}))
	defer upstream.Close()

	routes, err := routesFromConfig([]config.RouteConfig{
		{
			Name:        "websocket",
			Protocol:    "http",
			PathPrefix:  "/",
			UpstreamURL: upstream.URL,
		},
	})
	if err != nil {
		t.Fatalf("routesFromConfig() error = %v", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		t.Fatalf("router.New() error = %v", err)
	}
	registry := prometheus.NewRegistry()
	routeHandlers, err := handlersFromRoutes(
		routes,
		http.DefaultTransport,
		testCircuitFailureThreshold,
		testCircuitOpenTimeout,
		metrics.NewCircuitBreaker(registry),
		metrics.NewRateLimiter(registry),
		nil,
		discardLogger(),
	)
	if err != nil {
		t.Fatalf("handlersFromRoutes() error = %v", err)
	}
	logger := discardLogger()
	gatewayHandler := gateway.New(
		routeRouter,
		routeHandlers,
		logger,
		metrics.NewHTTP(registry),
	)
	handler := newHTTPMux(
		requestid.Middleware(gatewayHandler, logger),
		metrics.Handler(registry),
	)
	gatewayServer := httptest.NewServer(handler)
	defer gatewayServer.Close()

	websocketURL := "ws" + strings.TrimPrefix(gatewayServer.URL, "http")
	connection, err := websocket.Dial(websocketURL, "", gatewayServer.URL)
	if err != nil {
		t.Fatalf("dial WebSocket through gateway: %v", err)
	}
	defer func() {
		_ = connection.Close()
	}()

	if err := websocket.Message.Send(connection, message); err != nil {
		t.Fatalf("send WebSocket message: %v", err)
	}
	var received string
	if err := websocket.Message.Receive(connection, &received); err != nil {
		t.Fatalf("receive WebSocket message: %v", err)
	}
	if received != message {
		t.Errorf("received WebSocket message = %q, want %q", received, message)
	}

	select {
	case err := <-upstreamDone:
		if err != nil {
			t.Fatalf("upstream WebSocket exchange: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upstream WebSocket handler did not finish")
	}
}
