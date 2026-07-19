package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/proxy"
)

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		slog.Error("server stopped with an error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	upstreamURL, err := url.Parse(cfg.UpstreamURL)
	if err != nil {
		return fmt.Errorf("parse upstream URL: %w", err)
	}
	proxyHandler := proxy.New(upstreamURL)
	mux := newHTTPMux(proxyHandler)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	return serve(ctx, server, cfg.ShutdownTimeout)
}

func serve(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
	shutdownSignal, stop := signal.NotifyContext(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serveErrCh := make(chan error, 1)

	slog.Info("starting server", "addr", server.Addr)
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
		slog.Info("shutdown requested")

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

func newHTTPMux(proxyHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("/healthz", methodNotAllowedHandler)
	mux.Handle("/", proxyHandler)

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
