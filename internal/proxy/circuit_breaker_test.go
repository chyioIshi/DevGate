package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewCircuitBreakerTransportStoresConfiguration(t *testing.T) {
	base := testRoundTripper{}
	const failureThreshold = 3
	const openTimeout = 30 * time.Second

	transport, err := NewCircuitBreakerTransport(base, failureThreshold, openTimeout)
	if err != nil {
		t.Fatalf("NewCircuitBreakerTransport() error = %v", err)
	}
	if transport == nil {
		t.Fatal("NewCircuitBreakerTransport() transport = nil, want transport")
	}
	if transport.base != base {
		t.Errorf("base = %T, want original base %T", transport.base, base)
	}
	if transport.failureThreshold != failureThreshold {
		t.Errorf("failureThreshold = %d, want %d", transport.failureThreshold, failureThreshold)
	}
	if transport.openTimeout != openTimeout {
		t.Errorf("openTimeout = %s, want %s", transport.openTimeout, openTimeout)
	}
	if transport.now == nil {
		t.Fatal("now function = nil, want time source")
	}
	if transport.state != circuitClosed {
		t.Errorf("initial state = %d, want circuitClosed", transport.state)
	}
	if transport.consecutiveFailures != 0 {
		t.Errorf("initial consecutiveFailures = %d, want 0", transport.consecutiveFailures)
	}
	if !transport.openedAt.IsZero() {
		t.Errorf("initial openedAt = %s, want zero time", transport.openedAt)
	}
}

func TestNewCircuitBreakerTransportRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name             string
		base             http.RoundTripper
		failureThreshold int
		openTimeout      time.Duration
		wantMessage      string
	}{
		{
			name:             "nil base",
			failureThreshold: 1,
			openTimeout:      time.Second,
			wantMessage:      "base transport must not be nil",
		},
		{
			name:        "zero failure threshold",
			base:        testRoundTripper{},
			openTimeout: time.Second,
			wantMessage: "failure threshold must be positive",
		},
		{
			name:             "negative failure threshold",
			base:             testRoundTripper{},
			failureThreshold: -1,
			openTimeout:      time.Second,
			wantMessage:      "failure threshold must be positive",
		},
		{
			name:             "zero open timeout",
			base:             testRoundTripper{},
			failureThreshold: 1,
			wantMessage:      "circuit open timeout must be positive",
		},
		{
			name:             "negative open timeout",
			base:             testRoundTripper{},
			failureThreshold: 1,
			openTimeout:      -time.Second,
			wantMessage:      "circuit open timeout must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport, err := NewCircuitBreakerTransport(
				test.base,
				test.failureThreshold,
				test.openTimeout,
			)
			if err == nil {
				t.Fatal("NewCircuitBreakerTransport() error = nil, want validation error")
			}
			if transport != nil {
				t.Errorf("NewCircuitBreakerTransport() transport = %v, want nil", transport)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewCircuitBreakerTransport() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestClassifyCircuitOutcome(t *testing.T) {
	transportErr := errors.New("transport failed")
	tests := []struct {
		name          string
		response      *http.Response
		err           error
		cancelContext bool
		want          circuitOutcome
	}{
		{
			name: "nil result",
			want: circuitOutcomeIgnored,
		},
		{
			name:     "successful response",
			response: &http.Response{StatusCode: http.StatusOK},
			want:     circuitOutcomeSuccess,
		},
		{
			name:     "client error response",
			response: &http.Response{StatusCode: http.StatusNotFound},
			want:     circuitOutcomeSuccess,
		},
		{
			name:     "lower server error boundary",
			response: &http.Response{StatusCode: 500},
			want:     circuitOutcomeFailure,
		},
		{
			name:     "upper server error boundary",
			response: &http.Response{StatusCode: 599},
			want:     circuitOutcomeFailure,
		},
		{
			name:     "status above server error range",
			response: &http.Response{StatusCode: 600},
			want:     circuitOutcomeSuccess,
		},
		{
			name: "transport error",
			err:  transportErr,
			want: circuitOutcomeFailure,
		},
		{
			name:          "canceled context with transport error",
			err:           transportErr,
			cancelContext: true,
			want:          circuitOutcomeIgnored,
		},
		{
			name:          "canceled context with server error response",
			response:      &http.Response{StatusCode: http.StatusServiceUnavailable},
			cancelContext: true,
			want:          circuitOutcomeIgnored,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.cancelContext {
				canceledCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceledCtx
			}
			request := (&http.Request{}).WithContext(ctx)

			if got := classifyCircuitOutcome(request, test.response, test.err); got != test.want {
				t.Errorf("classifyCircuitOutcome() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCircuitBreakerAcquirePermitClosed(t *testing.T) {
	transport := &CircuitBreakerTransport{
		generation: 7,
		state:      circuitClosed,
	}

	permit, err := transport.acquirePermit()
	if err != nil {
		t.Fatalf("acquirePermit() error = %v", err)
	}
	want := circuitPermit{generation: 7, probe: false}
	if permit != want {
		t.Errorf("acquirePermit() permit = %+v, want %+v", permit, want)
	}
	if transport.state != circuitClosed {
		t.Errorf("state = %d, want circuitClosed", transport.state)
	}
}

func TestCircuitBreakerAcquirePermitOpenBeforeCooldown(t *testing.T) {
	openedAt := time.Unix(1_000, 0)
	const openTimeout = 10 * time.Second
	transport := &CircuitBreakerTransport{
		generation:  3,
		state:       circuitOpen,
		openTimeout: openTimeout,
		openedAt:    openedAt,
		now: func() time.Time {
			return openedAt.Add(openTimeout - time.Nanosecond)
		},
	}

	permit, err := transport.acquirePermit()
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("acquirePermit() error = %v, want ErrCircuitOpen", err)
	}
	if permit != (circuitPermit{}) {
		t.Errorf("acquirePermit() permit = %+v, want zero permit", permit)
	}
	if transport.state != circuitOpen {
		t.Errorf("state = %d, want circuitOpen", transport.state)
	}
}

func TestCircuitBreakerAcquirePermitOpenAtCooldownBoundary(t *testing.T) {
	openedAt := time.Unix(1_000, 0)
	const openTimeout = 10 * time.Second
	transport := &CircuitBreakerTransport{
		generation:  5,
		state:       circuitOpen,
		openTimeout: openTimeout,
		openedAt:    openedAt,
		now: func() time.Time {
			return openedAt.Add(openTimeout)
		},
	}

	permit, err := transport.acquirePermit()
	if err != nil {
		t.Fatalf("acquirePermit() error = %v", err)
	}
	want := circuitPermit{generation: 5, probe: true}
	if permit != want {
		t.Errorf("acquirePermit() permit = %+v, want %+v", permit, want)
	}
	if transport.state != circuitHalfOpen {
		t.Errorf("state = %d, want circuitHalfOpen", transport.state)
	}
}

func TestCircuitBreakerAcquirePermitHalfOpen(t *testing.T) {
	transport := &CircuitBreakerTransport{
		generation: 9,
		state:      circuitHalfOpen,
	}

	permit, err := transport.acquirePermit()
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("acquirePermit() error = %v, want ErrCircuitOpen", err)
	}
	if permit != (circuitPermit{}) {
		t.Errorf("acquirePermit() permit = %+v, want zero permit", permit)
	}
	if transport.state != circuitHalfOpen {
		t.Errorf("state = %d, want circuitHalfOpen", transport.state)
	}
}

func TestCircuitBreakerAcquirePermitAllowsOneHalfOpenProbeConcurrently(t *testing.T) {
	openedAt := time.Unix(1_000, 0)
	const (
		openTimeout  = 10 * time.Second
		requestCount = 64
	)
	transport := &CircuitBreakerTransport{
		generation:  11,
		state:       circuitOpen,
		openTimeout: openTimeout,
		openedAt:    openedAt,
		now: func() time.Time {
			return openedAt.Add(openTimeout)
		},
	}

	type result struct {
		permit circuitPermit
		err    error
	}
	start := make(chan struct{})
	results := make(chan result, requestCount)

	for range requestCount {
		go func() {
			<-start
			permit, err := transport.acquirePermit()
			results <- result{permit: permit, err: err}
		}()
	}
	close(start)

	var allowed int
	var rejected int
	for range requestCount {
		result := <-results
		switch {
		case result.err == nil:
			allowed++
			want := circuitPermit{generation: 11, probe: true}
			if result.permit != want {
				t.Errorf("allowed permit = %+v, want %+v", result.permit, want)
			}
		case errors.Is(result.err, ErrCircuitOpen):
			rejected++
			if result.permit != (circuitPermit{}) {
				t.Errorf("rejected permit = %+v, want zero permit", result.permit)
			}
		default:
			t.Errorf("acquirePermit() error = %v, want nil or ErrCircuitOpen", result.err)
		}
	}

	if allowed != 1 {
		t.Errorf("allowed requests = %d, want 1", allowed)
	}
	if rejected != requestCount-1 {
		t.Errorf("rejected requests = %d, want %d", rejected, requestCount-1)
	}
	if transport.state != circuitHalfOpen {
		t.Errorf("state = %d, want circuitHalfOpen", transport.state)
	}
}

func TestCircuitBreakerRecordOutcomeForClosedCircuit(t *testing.T) {
	now := time.Unix(2_000, 0)
	tests := []struct {
		name            string
		initialFailures int
		outcome         circuitOutcome
		wantState       circuitState
		wantFailures    int
		wantGeneration  uint64
		wantOpenedAt    time.Time
	}{
		{
			name:            "success resets consecutive failures",
			initialFailures: 2,
			outcome:         circuitOutcomeSuccess,
			wantState:       circuitClosed,
			wantGeneration:  7,
		},
		{
			name:            "ignored outcome preserves consecutive failures",
			initialFailures: 2,
			outcome:         circuitOutcomeIgnored,
			wantState:       circuitClosed,
			wantFailures:    2,
			wantGeneration:  7,
		},
		{
			name:            "failure below threshold increments counter",
			initialFailures: 1,
			outcome:         circuitOutcomeFailure,
			wantState:       circuitClosed,
			wantFailures:    2,
			wantGeneration:  7,
		},
		{
			name:            "failure reaching threshold opens circuit",
			initialFailures: 2,
			outcome:         circuitOutcomeFailure,
			wantState:       circuitOpen,
			wantGeneration:  8,
			wantOpenedAt:    now,
		},
		{
			name:            "unknown outcome changes nothing",
			initialFailures: 2,
			outcome:         circuitOutcome(99),
			wantState:       circuitClosed,
			wantFailures:    2,
			wantGeneration:  7,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &CircuitBreakerTransport{
				failureThreshold:    3,
				now:                 func() time.Time { return now },
				generation:          7,
				state:               circuitClosed,
				consecutiveFailures: test.initialFailures,
			}
			permit := circuitPermit{generation: 7}

			transport.recordOutcome(permit, test.outcome)

			if transport.state != test.wantState {
				t.Errorf("state = %d, want %d", transport.state, test.wantState)
			}
			if transport.consecutiveFailures != test.wantFailures {
				t.Errorf(
					"consecutiveFailures = %d, want %d",
					transport.consecutiveFailures,
					test.wantFailures,
				)
			}
			if transport.generation != test.wantGeneration {
				t.Errorf("generation = %d, want %d", transport.generation, test.wantGeneration)
			}
			if transport.openedAt != test.wantOpenedAt {
				t.Errorf("openedAt = %s, want %s", transport.openedAt, test.wantOpenedAt)
			}
		})
	}
}

func TestCircuitBreakerRecordOutcomeForHalfOpenProbe(t *testing.T) {
	openedAt := time.Unix(2_000, 0)
	now := openedAt.Add(30 * time.Second)
	tests := []struct {
		name           string
		outcome        circuitOutcome
		wantState      circuitState
		wantFailures   int
		wantGeneration uint64
		wantOpenedAt   time.Time
	}{
		{
			name:           "success closes circuit",
			outcome:        circuitOutcomeSuccess,
			wantState:      circuitClosed,
			wantGeneration: 10,
		},
		{
			name:           "failure reopens circuit and restarts cooldown",
			outcome:        circuitOutcomeFailure,
			wantState:      circuitOpen,
			wantGeneration: 10,
			wantOpenedAt:   now,
		},
		{
			name:           "ignored outcome releases probe without restarting cooldown",
			outcome:        circuitOutcomeIgnored,
			wantState:      circuitOpen,
			wantGeneration: 10,
			wantOpenedAt:   openedAt,
		},
		{
			name:           "unknown outcome changes nothing",
			outcome:        circuitOutcome(99),
			wantState:      circuitHalfOpen,
			wantFailures:   4,
			wantGeneration: 9,
			wantOpenedAt:   openedAt,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &CircuitBreakerTransport{
				now:                 func() time.Time { return now },
				generation:          9,
				state:               circuitHalfOpen,
				consecutiveFailures: 4,
				openedAt:            openedAt,
			}
			permit := circuitPermit{generation: 9, probe: true}

			transport.recordOutcome(permit, test.outcome)

			if transport.state != test.wantState {
				t.Errorf("state = %d, want %d", transport.state, test.wantState)
			}
			if transport.consecutiveFailures != test.wantFailures {
				t.Errorf(
					"consecutiveFailures = %d, want %d",
					transport.consecutiveFailures,
					test.wantFailures,
				)
			}
			if transport.generation != test.wantGeneration {
				t.Errorf("generation = %d, want %d", transport.generation, test.wantGeneration)
			}
			if transport.openedAt != test.wantOpenedAt {
				t.Errorf("openedAt = %s, want %s", transport.openedAt, test.wantOpenedAt)
			}
		})
	}
}

func TestCircuitBreakerRecordOutcomeIgnoresStaleGeneration(t *testing.T) {
	openedAt := time.Unix(2_000, 0)
	transport := &CircuitBreakerTransport{
		failureThreshold:    1,
		now:                 func() time.Time { return openedAt.Add(time.Minute) },
		generation:          4,
		state:               circuitClosed,
		consecutiveFailures: 2,
		openedAt:            openedAt,
	}
	stalePermit := circuitPermit{generation: 3}

	transport.recordOutcome(stalePermit, circuitOutcomeFailure)

	if transport.state != circuitClosed {
		t.Errorf("state = %d, want circuitClosed", transport.state)
	}
	if transport.generation != 4 {
		t.Errorf("generation = %d, want 4", transport.generation)
	}
	if transport.consecutiveFailures != 2 {
		t.Errorf("consecutiveFailures = %d, want 2", transport.consecutiveFailures)
	}
	if transport.openedAt != openedAt {
		t.Errorf("openedAt = %s, want %s", transport.openedAt, openedAt)
	}
}

func TestCircuitBreakerRecordOutcomeRejectsPermitForWrongState(t *testing.T) {
	tests := []struct {
		name   string
		state  circuitState
		permit circuitPermit
	}{
		{
			name:   "normal permit while circuit is open",
			state:  circuitOpen,
			permit: circuitPermit{generation: 6},
		},
		{
			name:   "normal permit while circuit is half-open",
			state:  circuitHalfOpen,
			permit: circuitPermit{generation: 6},
		},
		{
			name:   "probe permit while circuit is closed",
			state:  circuitClosed,
			permit: circuitPermit{generation: 6, probe: true},
		},
		{
			name:   "probe permit while circuit is open",
			state:  circuitOpen,
			permit: circuitPermit{generation: 6, probe: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			openedAt := time.Unix(2_000, 0)
			transport := &CircuitBreakerTransport{
				failureThreshold:    1,
				now:                 func() time.Time { return openedAt.Add(time.Minute) },
				generation:          6,
				state:               test.state,
				consecutiveFailures: 2,
				openedAt:            openedAt,
			}

			transport.recordOutcome(test.permit, circuitOutcomeFailure)

			if transport.state != test.state {
				t.Errorf("state = %d, want %d", transport.state, test.state)
			}
			if transport.generation != 6 {
				t.Errorf("generation = %d, want 6", transport.generation)
			}
			if transport.consecutiveFailures != 2 {
				t.Errorf("consecutiveFailures = %d, want 2", transport.consecutiveFailures)
			}
			if transport.openedAt != openedAt {
				t.Errorf("openedAt = %s, want %s", transport.openedAt, openedAt)
			}
		})
	}
}

func TestCircuitBreakerRecordOutcomeOpensOnceConcurrently(t *testing.T) {
	now := time.Unix(2_000, 0)
	const (
		failureThreshold = 8
		requestCount     = 64
	)
	transport := &CircuitBreakerTransport{
		failureThreshold: failureThreshold,
		now:              func() time.Time { return now },
		generation:       12,
		state:            circuitClosed,
	}
	permit := circuitPermit{generation: 12}
	done := make(chan struct{}, requestCount)

	for range requestCount {
		go func() {
			transport.recordOutcome(permit, circuitOutcomeFailure)
			done <- struct{}{}
		}()
	}
	for range requestCount {
		<-done
	}

	if transport.state != circuitOpen {
		t.Errorf("state = %d, want circuitOpen", transport.state)
	}
	if transport.generation != 13 {
		t.Errorf("generation = %d, want 13", transport.generation)
	}
	if transport.consecutiveFailures != 0 {
		t.Errorf("consecutiveFailures = %d, want 0", transport.consecutiveFailures)
	}
	if transport.openedAt != now {
		t.Errorf("openedAt = %s, want %s", transport.openedAt, now)
	}
}

func TestCircuitBreakerRoundTripForwardsAllowedRequest(t *testing.T) {
	wantResponse := &http.Response{StatusCode: http.StatusNoContent}
	base := &recordingRoundTripper{response: wantResponse}
	transport, err := NewCircuitBreakerTransport(base, 3, time.Minute)
	if err != nil {
		t.Fatalf("NewCircuitBreakerTransport() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)

	response, err := transport.RoundTrip(request)

	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if response != wantResponse {
		t.Errorf("RoundTrip() response = %p, want %p", response, wantResponse)
	}
	if base.calls != 1 {
		t.Errorf("base RoundTrip() calls = %d, want 1", base.calls)
	}
	if base.request != request {
		t.Errorf("base request = %p, want original request %p", base.request, request)
	}
}

func TestCircuitBreakerRoundTripOpensAfterTransportFailures(t *testing.T) {
	upstreamErr := errors.New("upstream unavailable")
	base := &recordingRoundTripper{err: upstreamErr}
	transport, err := NewCircuitBreakerTransport(base, 2, time.Minute)
	if err != nil {
		t.Fatalf("NewCircuitBreakerTransport() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)

	for attempt := 1; attempt <= 2; attempt++ {
		response, err := transport.RoundTrip(request)
		if response != nil {
			t.Errorf("attempt %d response = %v, want nil", attempt, response)
		}
		if !errors.Is(err, upstreamErr) {
			t.Errorf("attempt %d error = %v, want upstream error", attempt, err)
		}
	}

	response, err := transport.RoundTrip(request)
	if response != nil {
		t.Errorf("rejected response = %v, want nil", response)
	}
	if !errors.Is(err, ErrCircuitOpen) {
		t.Errorf("rejected error = %v, want ErrCircuitOpen", err)
	}
	if base.calls != 2 {
		t.Errorf("base RoundTrip() calls = %d, want 2", base.calls)
	}
	if transport.state != circuitOpen {
		t.Errorf("state = %d, want circuitOpen", transport.state)
	}
}

func TestCircuitBreakerRoundTripCountsServerErrorResponse(t *testing.T) {
	wantResponse := &http.Response{StatusCode: http.StatusServiceUnavailable}
	base := &recordingRoundTripper{response: wantResponse}
	transport, err := NewCircuitBreakerTransport(base, 1, time.Minute)
	if err != nil {
		t.Fatalf("NewCircuitBreakerTransport() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)

	response, err := transport.RoundTrip(request)

	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if response != wantResponse {
		t.Errorf("RoundTrip() response = %p, want %p", response, wantResponse)
	}
	if transport.state != circuitOpen {
		t.Errorf("state = %d, want circuitOpen", transport.state)
	}
	if base.calls != 1 {
		t.Errorf("base RoundTrip() calls = %d, want 1", base.calls)
	}
}

func TestCircuitBreakerRoundTripAllowsAnotherProbeAfterCanceledProbe(t *testing.T) {
	openedAt := time.Unix(3_000, 0)
	const openTimeout = 10 * time.Second
	wantResponse := &http.Response{StatusCode: http.StatusOK}
	base := &sequenceRoundTripper{
		results: []roundTripResult{
			{err: context.Canceled},
			{response: wantResponse},
		},
	}
	transport := &CircuitBreakerTransport{
		base:             base,
		failureThreshold: 1,
		openTimeout:      openTimeout,
		now: func() time.Time {
			return openedAt.Add(openTimeout)
		},
		generation: 4,
		state:      circuitOpen,
		openedAt:   openedAt,
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledRequest := httptest.NewRequest(
		http.MethodGet,
		"http://gateway.local/users",
		nil,
	).WithContext(canceledCtx)

	response, err := transport.RoundTrip(canceledRequest)
	if response != nil {
		t.Errorf("canceled probe response = %v, want nil", response)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled probe error = %v, want context.Canceled", err)
	}
	if transport.state != circuitOpen {
		t.Errorf("state after canceled probe = %d, want circuitOpen", transport.state)
	}
	if transport.openedAt != openedAt {
		t.Errorf("openedAt after canceled probe = %s, want unchanged %s", transport.openedAt, openedAt)
	}

	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/users", nil)
	response, err = transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("replacement probe error = %v", err)
	}
	if response != wantResponse {
		t.Errorf("replacement probe response = %p, want %p", response, wantResponse)
	}
	if transport.state != circuitClosed {
		t.Errorf("state after successful probe = %d, want circuitClosed", transport.state)
	}
	if transport.generation != 6 {
		t.Errorf("generation = %d, want 6", transport.generation)
	}
	if len(base.requests) != 2 {
		t.Errorf("base RoundTrip() calls = %d, want 2", len(base.requests))
	}
}
