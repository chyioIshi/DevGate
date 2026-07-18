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
)

const (
	serverAddr        = ":8080"
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
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
	mux := newHTTPMux()

	server := &http.Server{
		Addr:              serverAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	return serve(ctx, server)
}

func serve(ctx context.Context, server *http.Server) error {
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

func newHTTPMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)

	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}
