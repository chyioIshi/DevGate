package main

import (
	"context"
	"testing"
	"time"
)

func TestRouteRuntimeStartsAndStopsHealthRunners(t *testing.T) {
	t.Parallel()

	first := newBlockingHealthRunner()
	second := newBlockingHealthRunner()
	runtime := &routeRuntime{
		healthRunners: []healthRunner{first, second},
	}

	stop := runtime.startHealthRunners(context.Background())
	waitForRuntimeSignal(t, first.started, "first health runner did not start")
	waitForRuntimeSignal(t, second.started, "second health runner did not start")
	for i, runner := range []*blockingHealthRunner{first, second} {
		select {
		case <-runner.stopped:
			t.Errorf("health runner %d stopped before cleanup", i)
		default:
		}
	}

	cleanupDone := make(chan struct{})
	go func() {
		stop()
		close(cleanupDone)
	}()
	waitForRuntimeSignal(t, cleanupDone, "health-runner cleanup did not return")
	waitForRuntimeSignal(t, first.stopped, "first health runner did not stop")
	waitForRuntimeSignal(t, second.stopped, "second health runner did not stop")
}

type blockingHealthRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func newBlockingHealthRunner() *blockingHealthRunner {
	return &blockingHealthRunner{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (r *blockingHealthRunner) Run(ctx context.Context) {
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
