package upstream

import (
	"net/url"
	"strings"
	"testing"
)

func TestNewEndpointHealthRejectsInvalidThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		successThreshold int
		failureThreshold int
		wantMessage      string
	}{
		{
			name:             "zero success threshold",
			successThreshold: 0,
			failureThreshold: 1,
			wantMessage:      "success threshold",
		},
		{
			name:             "negative success threshold",
			successThreshold: -1,
			failureThreshold: 1,
			wantMessage:      "success threshold",
		},
		{
			name:             "zero failure threshold",
			successThreshold: 1,
			failureThreshold: 0,
			wantMessage:      "failure threshold",
		},
		{
			name:             "negative failure threshold",
			successThreshold: 1,
			failureThreshold: -1,
			wantMessage:      "failure threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := newEndpointHealth(
				url.URL{Scheme: "http", Host: "upstream.example"},
				tt.successThreshold,
				tt.failureThreshold,
			)
			if err == nil {
				t.Fatalf("newEndpointHealth() error = nil, want error containing %q", tt.wantMessage)
			}
			if got != nil {
				t.Errorf("newEndpointHealth() health = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("newEndpointHealth() error = %q, want it to contain %q", err, tt.wantMessage)
			}
		})
	}
}

func TestNewEndpointHealthStartsHealthy(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "upstream.example"}
	health, err := newEndpointHealth(target, 2, 3)
	if err != nil {
		t.Fatalf("newEndpointHealth() error = %v", err)
	}
	if !health.isHealthy() {
		t.Error("new endpoint is unhealthy, want healthy")
	}
	if health.target != target {
		t.Errorf("target = %q, want %q", &health.target, &target)
	}
}

func TestEndpointHealthTransitionsAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()

	health := newTestEndpointHealth(t, 2, 3)
	for attempt := 1; attempt < 3; attempt++ {
		if changed := health.recordFailure(); changed {
			t.Errorf("failure %d changed health before threshold", attempt)
		}
		if !health.isHealthy() {
			t.Fatalf("endpoint became unhealthy after failure %d, want threshold 3", attempt)
		}
	}

	if changed := health.recordFailure(); !changed {
		t.Error("failure 3 did not report a health transition")
	}
	if health.isHealthy() {
		t.Error("endpoint remains healthy after reaching failure threshold")
	}
	if changed := health.recordFailure(); changed {
		t.Error("failure while already unhealthy reported another transition")
	}
}

func TestEndpointHealthSuccessBreaksFailureSequence(t *testing.T) {
	t.Parallel()

	health := newTestEndpointHealth(t, 2, 3)
	health.recordFailure()
	health.recordFailure()
	health.recordSuccess()

	health.recordFailure()
	health.recordFailure()
	if !health.isHealthy() {
		t.Error("endpoint became unhealthy from non-consecutive failures")
	}
	if changed := health.recordFailure(); !changed {
		t.Error("third consecutive failure did not report a transition")
	}
}

func TestEndpointHealthTransitionsAfterConsecutiveSuccesses(t *testing.T) {
	t.Parallel()

	health := newTestEndpointHealth(t, 2, 1)
	if changed := health.recordFailure(); !changed {
		t.Fatal("failure did not transition endpoint to unhealthy")
	}
	if changed := health.recordSuccess(); changed {
		t.Error("first success changed health before threshold")
	}
	if health.isHealthy() {
		t.Fatal("endpoint recovered after one success, want threshold 2")
	}
	if changed := health.recordSuccess(); !changed {
		t.Error("second success did not report a health transition")
	}
	if !health.isHealthy() {
		t.Error("endpoint remains unhealthy after reaching success threshold")
	}
	if changed := health.recordSuccess(); changed {
		t.Error("success while already healthy reported another transition")
	}
}

func TestEndpointHealthFailureBreaksSuccessSequence(t *testing.T) {
	t.Parallel()

	health := newTestEndpointHealth(t, 3, 1)
	health.recordFailure()
	health.recordSuccess()
	health.recordSuccess()
	health.recordFailure()

	health.recordSuccess()
	health.recordSuccess()
	if health.isHealthy() {
		t.Error("endpoint recovered from non-consecutive successes")
	}
	if changed := health.recordSuccess(); !changed {
		t.Error("third consecutive success did not report a transition")
	}
}

func newTestEndpointHealth(
	t *testing.T,
	successThreshold int,
	failureThreshold int,
) *endpointHealth {
	t.Helper()

	health, err := newEndpointHealth(
		url.URL{Scheme: "http", Host: "upstream.example"},
		successThreshold,
		failureThreshold,
	)
	if err != nil {
		t.Fatalf("newEndpointHealth() error = %v", err)
	}
	return health
}
