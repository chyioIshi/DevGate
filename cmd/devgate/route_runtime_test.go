package main

import (
	"context"
	"testing"
	"time"
)

func TestRouteRuntimeStartsAndStopsHealthChecks(t *testing.T) {
	t.Parallel()

	first := newBlockingHealthCheckRunner()
	second := newBlockingHealthCheckRunner()
	runtime := &routeRuntime{
		healthCheckers: []healthCheckRunner{first, second},
	}

	stop := runtime.startHealthChecks(context.Background())
	waitForRuntimeSignal(t, first.started, "first health checker did not start")
	waitForRuntimeSignal(t, second.started, "second health checker did not start")
	for i, runner := range []*blockingHealthCheckRunner{first, second} {
		select {
		case <-runner.stopped:
			t.Errorf("health checker %d stopped before cleanup", i)
		default:
		}
	}

	cleanupDone := make(chan struct{})
	go func() {
		stop()
		close(cleanupDone)
	}()
	waitForRuntimeSignal(t, cleanupDone, "health-check cleanup did not return")
	waitForRuntimeSignal(t, first.stopped, "first health checker did not stop")
	waitForRuntimeSignal(t, second.stopped, "second health checker did not stop")
}

type blockingHealthCheckRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func newBlockingHealthCheckRunner() *blockingHealthCheckRunner {
	return &blockingHealthCheckRunner{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (r *blockingHealthCheckRunner) Run(ctx context.Context) {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
}

func waitForRuntimeSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}
