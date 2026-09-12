package ratelimit

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/time/rate"
)

func TestNewLocalStoresConfiguration(t *testing.T) {
	const requestsPerSecond = 2.5
	const burst = 7

	limiter, err := NewLocal(requestsPerSecond, burst)
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}
	if limiter == nil {
		t.Fatal("NewLocal() limiter = nil, want limiter")
	}
	if got := limiter.limiter.Limit(); got != rate.Limit(requestsPerSecond) {
		t.Errorf("limit = %v, want %v", got, requestsPerSecond)
	}
	if got := limiter.limiter.Burst(); got != burst {
		t.Errorf("burst = %d, want %d", got, burst)
	}
}

func TestNewLocalRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name              string
		requestsPerSecond float64
		burst             int
		wantMessage       string
	}{
		{name: "zero rate", burst: 1, wantMessage: "must be positive"},
		{name: "negative rate", requestsPerSecond: -1, burst: 1, wantMessage: "must be positive"},
		{name: "NaN rate", requestsPerSecond: math.NaN(), burst: 1, wantMessage: "finite"},
		{name: "positive infinite rate", requestsPerSecond: math.Inf(1), burst: 1, wantMessage: "finite"},
		{name: "negative infinite rate", requestsPerSecond: math.Inf(-1), burst: 1, wantMessage: "positive"},
		{name: "zero burst", requestsPerSecond: 1, wantMessage: "burst must be positive"},
		{name: "negative burst", requestsPerSecond: 1, burst: -1, wantMessage: "burst must be positive"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limiter, err := NewLocal(test.requestsPerSecond, test.burst)
			if err == nil {
				t.Fatal("NewLocal() error = nil, want validation error")
			}
			if limiter != nil {
				t.Errorf("NewLocal() limiter = %v, want nil", limiter)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewLocal() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestLocalAllowConsumesInitialBurst(t *testing.T) {
	const burst = 3
	limiter, err := NewLocal(1e-9, burst)
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}

	for request := 1; request <= burst; request++ {
		if !limiter.Allow() {
			t.Errorf("Allow() request %d = false, want true", request)
		}
	}
	if limiter.Allow() {
		t.Error("Allow() after burst exhausted = true, want false")
	}
}

func TestLocalAllowLimitsConcurrentRequestsToBurst(t *testing.T) {
	const (
		burst    = 7
		attempts = 64
	)
	limiter, err := NewLocal(1e-9, burst)
	if err != nil {
		t.Fatalf("NewLocal() error = %v", err)
	}

	start := make(chan struct{})
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if limiter.Allow() {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != burst {
		t.Errorf("allowed requests = %d, want burst %d", got, burst)
	}
}
