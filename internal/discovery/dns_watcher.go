package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// EndpointResolver resolves a logical target into its current endpoint
// snapshot.
type EndpointResolver interface {
	Resolve(context.Context, url.URL) ([]url.URL, error)
}

// EndpointSnapshotApplier publishes a resolved endpoint snapshot.
type EndpointSnapshotApplier interface {
	Apply([]url.URL) error
}

// DNSRefreshObserver records the result of a DNS refresh attempt.
type DNSRefreshObserver interface {
	RecordRefresh(url.URL, int, error)
}

// DNSWatcher periodically resolves a target and publishes successful endpoint
// snapshots.
type DNSWatcher struct {
	resolver        EndpointResolver
	applier         EndpointSnapshotApplier
	observer        DNSRefreshObserver
	target          url.URL
	refreshInterval time.Duration
	lookupTimeout   time.Duration
}

// NewDNSWatcher creates a watcher from explicit resolution, publication, and
// observation dependencies and positive refresh timing settings.
func NewDNSWatcher(
	resolver EndpointResolver,
	applier EndpointSnapshotApplier,
	observer DNSRefreshObserver,
	target url.URL,
	refreshInterval time.Duration,
	lookupTimeout time.Duration,
) (*DNSWatcher, error) {
	if resolver == nil {
		return nil, errors.New("endpoint resolver was not provided")
	}
	if applier == nil {
		return nil, errors.New("endpoint snapshot applier was not provided")
	}
	if observer == nil {
		return nil, errors.New("DNS refresh observer was not provided")
	}
	if refreshInterval <= 0 {
		return nil, errors.New("refresh interval must be greater than 0")
	}
	if lookupTimeout <= 0 {
		return nil, errors.New("lookup timeout must be greater than 0")
	}
	return &DNSWatcher{
		resolver:        resolver,
		applier:         applier,
		observer:        observer,
		target:          target,
		refreshInterval: refreshInterval,
		lookupTimeout:   lookupTimeout,
	}, nil
}

// Run refreshes immediately and then after a fixed delay until ctx is
// canceled. Run blocks the calling goroutine.
func (w *DNSWatcher) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	w.refresh(ctx)
	timer := time.NewTimer(w.refreshInterval)

	for {
		select {
		case <-timer.C:
			w.refresh(ctx)
			timer.Reset(w.refreshInterval)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (w *DNSWatcher) refresh(ctx context.Context) {
	lookupCtx, cancel := context.WithTimeout(ctx, w.lookupTimeout)

	endpoints, err := w.resolver.Resolve(lookupCtx, w.target)
	cancel()

	if ctx.Err() != nil {
		return
	}
	if err != nil {
		w.observer.RecordRefresh(
			w.target,
			0,
			fmt.Errorf("resolve DNS endpoints: %w", err),
		)
		return
	}
	if err := w.applier.Apply(endpoints); err != nil {
		w.observer.RecordRefresh(
			w.target,
			len(endpoints),
			fmt.Errorf("apply DNS endpoint snapshot: %w", err),
		)
		return
	}
	w.observer.RecordRefresh(w.target, len(endpoints), nil)
}
