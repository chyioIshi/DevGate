package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/gateway"
	"github.com/chyioishi/devgate/internal/metrics"
	"github.com/chyioishi/devgate/internal/proxy"
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger, err := newLogger(os.Stdout, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		slog.Error("failed to create logger", "error", err)
		os.Exit(1)
	}
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("server stopped with an error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	routes, err := routesFromConfig(cfg.Routes)
	if err != nil {
		return fmt.Errorf("load routes from config: %w", err)
	}
	globalErrorResponder, err := globalErrorResponderFromConfig(cfg.ErrorResponses)
	if err != nil {
		return fmt.Errorf("load global error responses: %w", err)
	}
	routeRouter, err := router.New(routes)
	if err != nil {
		return fmt.Errorf("build routing table: %w", err)
	}
	upstreamTransport, err := proxy.NewTransport(cfg.UpstreamResponseHeaderTimeout)
	if err != nil {
		return fmt.Errorf("create upstream transport: %w", err)
	}
	defer upstreamTransport.CloseIdleConnections()

	healthCheckClient := &http.Client{
		Transport: upstreamTransport,
	}

	retryTransport, err := proxy.NewRetryTransport(upstreamTransport, cfg.UpstreamMaxAttempts, cfg.UpstreamRetryBaseDelay)
	if err != nil {
		return fmt.Errorf("create upstream retry transport: %w", err)
	}

	promRegistry := prometheus.NewRegistry()
	circuitBreakerMetrics := metrics.NewCircuitBreaker(promRegistry)
	rateLimiterMetrics := metrics.NewRateLimiter(promRegistry)
	httpMetrics := metrics.NewHTTP(promRegistry)

	routeRuntime, err := routeRuntimeFromRoutes(
		routes,
		retryTransport,
		healthCheckClient,
		cfg.UpstreamCircuitFailureThreshold,
		cfg.UpstreamCircuitOpenTimeout,
		circuitBreakerMetrics,
		rateLimiterMetrics,
		cfg.TrustedProxyCIDRs,
		logger,
	)
	if err != nil {
		return fmt.Errorf("create route handlers: %w", err)
	}

	gatewayHandler := gateway.New(
		routeRouter,
		routeRuntime.handlers,
		globalErrorResponder.Write,
		logger,
		httpMetrics,
	)
	requestIDHandler := requestid.Middleware(
		gatewayHandler,
		globalErrorResponder.Write,
		logger,
	)
	mux := newHTTPMux(
		requestIDHandler,
		metrics.Handler(promRegistry),
		globalErrorResponder.Write,
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		IdleTimeout:       cfg.IdleTimeout,
	}
	stopHealthChecks := routeRuntime.startHealthChecks(ctx)
	defer stopHealthChecks()

	return serve(ctx, server, logger, cfg.ShutdownTimeout)
}

func serve(ctx context.Context, server *http.Server, logger *slog.Logger, shutdownTimeout time.Duration) error {
	shutdownSignal, stop := signal.NotifyContext(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serveErrCh := make(chan error, 1)

	logger.Info("starting server", "addr", server.Addr)
	go func() {
		serveErrCh <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)

	case <-shutdownSignal.Done():
		stop()
		logger.Info("shutdown requested")

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			shutdownErr := fmt.Errorf("graceful shutdown: %w", err)

			if closeErr := server.Close(); closeErr != nil {
				return errors.Join(
					shutdownErr,
					fmt.Errorf("force close server: %w", closeErr),
				)
			}

			return shutdownErr
		}

		err := <-serveErrCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}

		return nil
	}
}

func newHTTPMux(
	gatewayHandler http.Handler,
	metricsHandler http.Handler,
	responder func(http.ResponseWriter, *http.Request, int),
) *http.ServeMux {
	mux := http.NewServeMux()
	notAllowedHandler := methodNotAllowedHandler(responder)
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("/healthz", notAllowedHandler)
	mux.Handle("GET /metrics", metricsHandler)
	mux.HandleFunc("/metrics", notAllowedHandler)
	mux.Handle("/", gatewayHandler)
	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func methodNotAllowedHandler(
	responder func(http.ResponseWriter, *http.Request, int),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		if responder != nil {
			responder(w, r, http.StatusMethodNotAllowed)
		} else {
			http.Error(
				w,
				http.StatusText(http.StatusMethodNotAllowed),
				http.StatusMethodNotAllowed,
			)
		}
	}
}
