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
	"github.com/chyioishi/devgate/internal/requestid"
	"github.com/chyioishi/devgate/internal/router"
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
	routeRouter, err := router.New(routes)
	if err != nil {
		return fmt.Errorf("build routing table: %w", err)
	}
	routeHandlers, err := handlersFromRoutes(routes, logger)
	if err != nil {
		return fmt.Errorf("create route handlers: %w", err)
	}

	gatewayHandler := gateway.New(routeRouter, routeHandlers, logger)
	requestIDHandler := requestid.Middleware(gatewayHandler, logger)
	mux := newHTTPMux(requestIDHandler)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

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

func newHTTPMux(gatewayHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("/healthz", methodNotAllowedHandler)
	mux.Handle("/", gatewayHandler)

	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}

func methodNotAllowedHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
