package ratelimit

import (
	"context"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestNewRedisStoresConfiguration(t *testing.T) {
	client := newTestRedisClient(t)

	limiter, err := NewRedis(client, "devgate:ratelimit:users", 2.5, 7)
	if err != nil {
		t.Fatalf("NewRedis() error = %v", err)
	}
	if limiter == nil {
		t.Fatal("NewRedis() limiter = nil, want limiter")
	}
	if limiter.client != client {
		t.Errorf("client = %p, want %p", limiter.client, client)
	}
	if got, want := limiter.key, "devgate:ratelimit:users"; got != want {
		t.Errorf("key = %q, want %q", got, want)
	}
	if got, want := limiter.requestsPerSecond, 2.5; got != want {
		t.Errorf("requests per second = %v, want %v", got, want)
	}
	if got, want := limiter.burst, 7; got != want {
		t.Errorf("burst = %d, want %d", got, want)
	}
	if got, want := limiter.ttlMilliseconds, int64(2800); got != want {
		t.Errorf("TTL milliseconds = %d, want %d", got, want)
	}
}

func TestNewRedisRejectsInvalidConfiguration(t *testing.T) {
	client := newTestRedisClient(t)
	tests := []struct {
		name              string
		client            *redis.Client
		key               string
		requestsPerSecond float64
		burst             int
		wantMessage       string
	}{
		{
			name:              "nil client",
			key:               "key",
			requestsPerSecond: 1,
			burst:             1,
			wantMessage:       "redis client",
		},
		{
			name:              "empty key",
			client:            client,
			requestsPerSecond: 1,
			burst:             1,
			wantMessage:       "key must not be empty",
		},
		{
			name:              "whitespace key",
			client:            client,
			key:               " \t ",
			requestsPerSecond: 1,
			burst:             1,
			wantMessage:       "key must not be empty",
		},
		{
			name:        "zero rate",
			client:      client,
			key:         "key",
			burst:       1,
			wantMessage: "must be positive",
		},
		{
			name:              "negative rate",
			client:            client,
			key:               "key",
			requestsPerSecond: -1,
			burst:             1,
			wantMessage:       "must be positive",
		},
		{
			name:              "NaN rate",
			client:            client,
			key:               "key",
			requestsPerSecond: math.NaN(),
			burst:             1,
			wantMessage:       "finite range",
		},
		{
			name:              "infinite rate",
			client:            client,
			key:               "key",
			requestsPerSecond: math.Inf(1),
			burst:             1,
			wantMessage:       "finite range",
		},
		{
			name:              "refill duration overflow",
			client:            client,
			key:               "key",
			requestsPerSecond: math.SmallestNonzeroFloat64,
			burst:             1,
			wantMessage:       "refill duration is too large",
		},
		{
			name:              "zero burst",
			client:            client,
			key:               "key",
			requestsPerSecond: 1,
			wantMessage:       "burst must be positive",
		},
		{
			name:              "negative burst",
			client:            client,
			key:               "key",
			requestsPerSecond: 1,
			burst:             -1,
			wantMessage:       "burst must be positive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limiter, err := NewRedis(
				test.client,
				test.key,
				test.requestsPerSecond,
				test.burst,
			)
			if err == nil {
				t.Fatal("NewRedis() error = nil, want validation error")
			}
			if limiter != nil {
				t.Errorf("NewRedis() limiter = %v, want nil", limiter)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewRedis() error = %q, want %q", err, test.wantMessage)
			}
		})
	}
}

func TestRedisAllowConsumesBurstAndRefills(t *testing.T) {
	server := miniredis.RunT(t)
	server.SetTime(time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC))
	client := newTestRedisClientWithAddr(t, server.Addr())
	const key = "devgate:ratelimit:users"
	limiter, err := NewRedis(client, key, 2, 3)
	if err != nil {
		t.Fatalf("NewRedis() error = %v", err)
	}

	for request := 1; request <= 3; request++ {
		assertRedisDecision(t, limiter, true, request)
	}
	assertRedisDecision(t, limiter, false, 4)

	if got, want := server.TTL(key), 1500*time.Millisecond; got != want {
		t.Errorf("bucket TTL = %s, want %s", got, want)
	}

	server.SetTime(time.Date(2026, time.November, 5, 12, 0, 0, 500_000_000, time.UTC))
	assertRedisDecision(t, limiter, true, 5)
	assertRedisDecision(t, limiter, false, 6)
}

func TestRedisAllowSharesBucketAcrossConcurrentRequests(t *testing.T) {
	server := miniredis.RunT(t)
	firstClient := newTestRedisClientWithAddr(t, server.Addr())
	secondClient := newTestRedisClientWithAddr(t, server.Addr())
	const (
		burst    = 7
		attempts = 64
	)
	firstLimiter, err := NewRedis(
		firstClient,
		"devgate:ratelimit:shared",
		1e-9,
		burst,
	)
	if err != nil {
		t.Fatalf("NewRedis() error = %v", err)
	}
	secondLimiter, err := NewRedis(
		secondClient,
		"devgate:ratelimit:shared",
		1e-9,
		burst,
	)
	if err != nil {
		t.Fatalf("NewRedis() error = %v", err)
	}
	limiters := []*Redis{firstLimiter, secondLimiter}

	start := make(chan struct{})
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for attempt := range attempts {
		wg.Add(1)
		go func(limiter *Redis) {
			defer wg.Done()
			<-start
			decision, allowErr := limiter.Allow(context.Background())
			if allowErr != nil {
				t.Errorf("Allow() error = %v", allowErr)
				return
			}
			if decision {
				allowed.Add(1)
			}
		}(limiters[attempt%len(limiters)])
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got != burst {
		t.Errorf("allowed requests = %d, want burst %d", got, burst)
	}
}

func TestRedisAllowReturnsBackendError(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTestRedisClientWithAddr(t, server.Addr())
	limiter, err := NewRedis(client, "devgate:ratelimit:users", 1, 1)
	if err != nil {
		t.Fatalf("NewRedis() error = %v", err)
	}
	server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	allowed, err := limiter.Allow(ctx)
	if err == nil {
		t.Fatal("Allow() error = nil, want Redis backend error")
	}
	if allowed {
		t.Error("Allow() = true after Redis failure, want false")
	}
	if !strings.Contains(err.Error(), "redis allow request") {
		t.Errorf("Allow() error = %q, want operation context", err)
	}
}

func assertRedisDecision(t *testing.T, limiter *Redis, want bool, request int) {
	t.Helper()

	got, err := limiter.Allow(context.Background())
	if err != nil {
		t.Fatalf("Allow() request %d error = %v", request, err)
	}
	if got != want {
		t.Errorf("Allow() request %d = %t, want %t", request, got, want)
	}
}

func newTestRedisClient(t *testing.T) *redis.Client {
	return newTestRedisClientWithAddr(t, "127.0.0.1:6379")
}

func newTestRedisClientWithAddr(t *testing.T, addr string) *redis.Client {
	t.Helper()

	client := redis.NewClient(&redis.Options{
		Addr:       addr,
		MaxRetries: -1,
	})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})
	return client
}
