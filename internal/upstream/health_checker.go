package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const probeResultBodyLimit int64 = 4 << 10 // 4 KB

// HealthChecker periodically probes tracked upstream endpoints.
type HealthChecker struct {
	client              *http.Client
	tracker             *HealthTracker
	interval            time.Duration
	timeout             time.Duration
	healthPath          string
	maxConcurrentProbes int
	observer            HealthCheckObserver
}

type healthCheckResult struct {
	target  url.URL
	healthy bool
	changed bool
	tracked bool
	err     error
}

// HealthCheckObserver records probe outcomes and endpoint health transitions.
// Implementations should return promptly because notifications are delivered
// synchronously between probe rounds.
type HealthCheckObserver interface {
	// RecordProbeResult records the outcome of one probe.
	RecordProbeResult(target url.URL, successful bool, err error)
	// RecordStateTransition records a completed transition to healthy or unhealthy.
	RecordStateTransition(target url.URL, healthy bool)
}

// NewHealthChecker creates a checker with explicit HTTP and health-state
// dependencies and positive probe timing configuration.
func NewHealthChecker(
	client *http.Client,
	tracker *HealthTracker,
	interval time.Duration,
	timeout time.Duration,
	healthPath string,
	maxConcurrentProbes int,
	observer HealthCheckObserver,
) (*HealthChecker, error) {
	if client == nil {
		return nil, errors.New("http client was not provided")
	}
	if tracker == nil {
		return nil, errors.New("health tracker was not provided")
	}
	if interval <= 0 {
		return nil, errors.New("interval must be greater than 0")
	}
	if timeout <= 0 {
		return nil, errors.New("timeout must be greater than 0")
	}
	if healthPath == "" {
		return nil, errors.New("health path was not provided")
	}
	if !strings.HasPrefix(healthPath, "/") {
		return nil, errors.New("health path must start with '/'")
	}
	if maxConcurrentProbes <= 0 {
		return nil, errors.New("max concurrent probes must be greater than 0")
	}
	if observer == nil {
		return nil, errors.New("health check observer was not provided")
	}
	return &HealthChecker{
		client:              client,
		tracker:             tracker,
		interval:            interval,
		timeout:             timeout,
		healthPath:          healthPath,
		maxConcurrentProbes: maxConcurrentProbes,
		observer:            observer,
	}, nil
}

// Run probes tracked endpoints immediately and then after a fixed delay until
// the context is canceled. Run blocks the calling goroutine.
func (c *HealthChecker) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.runRound(ctx)
			timer.Reset(c.interval)
		}
	}
}

func (c *HealthChecker) runRound(ctx context.Context) {
	var wg sync.WaitGroup
	targets := c.tracker.Targets()
	results := make([]healthCheckResult, len(targets))
	jobs := make(
		chan int,
		len(targets),
	)
	for i := range targets {
		jobs <- i
	}
	close(jobs)

	for range min(len(targets), c.maxConcurrentProbes) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for idx := range jobs {
				result := c.checkTarget(ctx, targets[idx])
				results[idx] = result
			}
		}()
	}
	wg.Wait()
	for _, result := range results {
		if result.tracked {
			c.observer.RecordProbeResult(result.target, result.healthy, result.err)
			if result.changed {
				c.observer.RecordStateTransition(result.target, result.healthy)
			}
		}
	}
}

func (c *HealthChecker) checkTarget(
	ctx context.Context,
	target url.URL,
) healthCheckResult {
	err := c.probe(ctx, target)

	if ctx.Err() != nil {
		return healthCheckResult{target: target, err: ctx.Err()}
	}
	if err != nil {
		changed, tracked := c.tracker.RecordFailure(target)
		return healthCheckResult{
			target:  target,
			changed: changed,
			tracked: tracked,
			err:     err,
		}
	}
	changed, tracked := c.tracker.RecordSuccess(target)
	return healthCheckResult{
		target:  target,
		healthy: true,
		changed: changed,
		tracked: tracked,
	}
}

func (c *HealthChecker) probe(ctx context.Context, target url.URL) error {
	probeURL := target
	probeURL.Path = c.healthPath

	probeURL.RawPath = ""
	probeURL.RawQuery = ""
	probeURL.Fragment = ""
	probeURL.ForceQuery = false

	probeCtx, cancel := context.WithTimeout(
		ctx,
		c.timeout,
	)
	defer cancel()
	req, err := http.NewRequestWithContext(
		probeCtx,
		http.MethodGet,
		probeURL.String(),
		nil,
	)
	if err != nil {
		return fmt.Errorf(
			"create health probe for URL %q: %w",
			probeURL.String(),
			err,
		)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf(
			"send health probe for URL %q: %w",
			probeURL.String(),
			err,
		)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	limitedBody := io.LimitReader(resp.Body, probeResultBodyLimit)
	if _, err := io.Copy(io.Discard, limitedBody); err != nil {
		return fmt.Errorf(
			"read health probe response body for URL %q: %w",
			probeURL.String(),
			err,
		)
	}

	if http.StatusOK > resp.StatusCode || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"health check failed with %d status code for URL %q",
			resp.StatusCode,
			probeURL.String(),
		)
	}
	return nil
}
