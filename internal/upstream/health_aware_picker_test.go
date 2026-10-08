package upstream

import (
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestNewHealthAwarePickerRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}

	tests := []struct {
		name        string
		picker      replaceablePicker
		tracker     *HealthTracker
		wantMessage string
	}{
		{
			name:        "missing picker",
			tracker:     tracker,
			wantMessage: "picker was not provided",
		},
		{
			name:        "missing tracker",
			picker:      &recordingReplaceablePicker{},
			wantMessage: "tracker was not provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewHealthAwarePicker(test.picker, test.tracker)
			if err == nil {
				t.Fatalf("NewHealthAwarePicker() error = nil, want %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("NewHealthAwarePicker() picker = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewHealthAwarePicker() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestNewHealthAwarePickerPublishesOnlyHealthyEndpoints(t *testing.T) {
	t.Parallel()

	healthy := url.URL{Scheme: "http", Host: "healthy.example"}
	unhealthy := url.URL{Scheme: "http", Host: "unhealthy.example"}
	tracker, err := NewHealthTracker([]url.URL{healthy, unhealthy}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	tracker.RecordFailure(unhealthy)
	base := &recordingReplaceablePicker{}

	picker, err := NewHealthAwarePicker(base, tracker)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}

	if got := base.lastReplacement(); !slices.Equal(got, []url.URL{healthy}) {
		t.Errorf("Replace() endpoints = %v, want %v", got, []url.URL{healthy})
	}
	target, release, err := picker.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	release()
	if target != healthy {
		t.Errorf("Acquire() target = %v, want %v", target, healthy)
	}
}

func TestHealthAwarePickerFailsClosedAndRecovers(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	base := &recordingReplaceablePicker{}
	picker, err := NewHealthAwarePicker(base, tracker)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}

	tracker.RecordFailure(target)
	if err := picker.Refresh(); err != nil {
		t.Fatalf("Refresh() with no healthy endpoints error = %v", err)
	}
	if got := base.replaceCallCount(); got != 1 {
		t.Errorf("Replace() calls after empty snapshot = %d, want 1", got)
	}
	gotTarget, release, err := picker.Acquire()
	if !errors.Is(err, ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", err)
	}
	if gotTarget != (url.URL{}) {
		t.Errorf("Acquire() target = %v, want zero URL", gotTarget)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}
	if got := base.acquireCallCount(); got != 0 {
		t.Errorf("base Acquire() calls = %d, want 0", got)
	}

	tracker.RecordSuccess(target)
	if err := picker.Refresh(); err != nil {
		t.Fatalf("Refresh() after recovery error = %v", err)
	}
	gotTarget, release, err = picker.Acquire()
	if err != nil {
		t.Fatalf("Acquire() after recovery error = %v", err)
	}
	release()
	if gotTarget != target {
		t.Errorf("Acquire() target after recovery = %v, want %v", gotTarget, target)
	}
}

func TestHealthAwarePickerFailsClosedWhenReplaceFails(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	base := &recordingReplaceablePicker{}
	picker, err := NewHealthAwarePicker(base, tracker)
	if err != nil {
		t.Fatalf("NewHealthAwarePicker() error = %v", err)
	}
	replaceErr := errors.New("replace failed")
	base.setReplaceError(replaceErr)

	err = picker.Refresh()
	if !errors.Is(err, replaceErr) {
		t.Errorf("Refresh() error = %v, want wrapped replace error", err)
	}
	if !strings.Contains(err.Error(), "replace healthy endpoints") {
		t.Errorf("Refresh() error = %q, want replace context", err)
	}
	_, release, acquireErr := picker.Acquire()
	if !errors.Is(acquireErr, ErrNoHealthyEndpoints) {
		t.Errorf("Acquire() error = %v, want ErrNoHealthyEndpoints", acquireErr)
	}
	if release != nil {
		t.Error("Acquire() release is non-nil, want nil")
	}
}

func TestNewHealthAwarePickerReturnsRefreshError(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	tracker, err := NewHealthTracker([]url.URL{target}, 1, 1)
	if err != nil {
		t.Fatalf("NewHealthTracker() error = %v", err)
	}
	replaceErr := errors.New("replace failed")
	base := &recordingReplaceablePicker{replaceErr: replaceErr}

	picker, err := NewHealthAwarePicker(base, tracker)
	if err == nil {
		t.Fatal("NewHealthAwarePicker() error = nil, want refresh error")
	}
	if picker != nil {
		t.Errorf("NewHealthAwarePicker() picker = %#v, want nil", picker)
	}
	if !errors.Is(err, replaceErr) {
		t.Errorf("NewHealthAwarePicker() error = %v, want wrapped replace error", err)
	}
	for _, context := range []string{"refresh health-aware picker", "replace healthy endpoints"} {
		if !strings.Contains(err.Error(), context) {
			t.Errorf("NewHealthAwarePicker() error = %q, want context %q", err, context)
		}
	}
}

type recordingReplaceablePicker struct {
	mu           sync.Mutex
	endpoints    []url.URL
	replaceErr   error
	replaceCalls int
	acquireCalls int
}

func (p *recordingReplaceablePicker) Acquire() (url.URL, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.acquireCalls++
	return p.endpoints[0], func() {}, nil
}

func (p *recordingReplaceablePicker) Replace(endpoints []url.URL) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.replaceCalls++
	if p.replaceErr != nil {
		return p.replaceErr
	}
	p.endpoints = slices.Clone(endpoints)
	return nil
}

func (p *recordingReplaceablePicker) lastReplacement() []url.URL {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.endpoints)
}

func (p *recordingReplaceablePicker) replaceCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.replaceCalls
}

func (p *recordingReplaceablePicker) acquireCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireCalls
}

func (p *recordingReplaceablePicker) setReplaceError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replaceErr = err
}
