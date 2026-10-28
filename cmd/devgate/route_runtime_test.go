package main

import (
	"context"
	"testing"
	"time"
)

func TestRouteRuntimeStartsAndStopsBackgroundRunners(t *testing.T) {
	t.Parallel()

	first := newBlockingBackgroundRunner()
	second := newBlockingBackgroundRunner()
	runtime := &routeRuntime{
		backgroundRunners: []backgroundRunner{first, second},
	}

	stop := runtime.startBackgroundRunners(context.Background())
	waitForRuntimeSignal(t, first.started, "first background runner did not start")
	waitForRuntimeSignal(t, second.started, "second background runner did not start")
	for i, runner := range []*blockingBackgroundRunner{first, second} {
		select {
		case <-runner.stopped:
			t.Errorf("background runner %d stopped before cleanup", i)
		default:
		}
	}

	cleanupDone := make(chan struct{})
	go func() {
		stop()
		close(cleanupDone)
	}()
	waitForRuntimeSignal(t, cleanupDone, "background-runner cleanup did not return")
	waitForRuntimeSignal(t, first.stopped, "first background runner did not stop")
	waitForRuntimeSignal(t, second.stopped, "second background runner did not stop")
}

type blockingBackgroundRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func newBlockingBackgroundRunner() *blockingBackgroundRunner {
	return &blockingBackgroundRunner{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (r *blockingBackgroundRunner) Run(ctx context.Context) {
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
