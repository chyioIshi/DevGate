package main

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/chyioishi/devgate/internal/ratelimit"
	"github.com/chyioishi/devgate/internal/router"
)

func TestNewRateLimiterCreatesLocalBackend(t *testing.T) {
	limiter, err := newRateLimiter(
		"users",
		router.RateLimitPolicy{
			Backend:           router.RateLimitBackendLocal,
			RequestsPerSecond: 2,
			Burst:             3,
		},
		nil,
		"devgate",
	)
	if err != nil {
		t.Fatalf("newRateLimiter() error = %v", err)
	}
	if _, ok := limiter.(*ratelimit.Local); !ok {
		t.Errorf("newRateLimiter() type = %T, want *ratelimit.Local", limiter)
	}
}

func TestNewRateLimiterCreatesNamespacedRedisBackend(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTestRedisClientWithAddr(t, server.Addr())
	limiter, err := newRateLimiter(
		"users",
		router.RateLimitPolicy{
			Backend:           router.RateLimitBackendRedis,
			RequestsPerSecond: 2,
			Burst:             3,
		},
		client,
		"prod:payments",
	)
	if err != nil {
		t.Fatalf("newRateLimiter() error = %v", err)
	}
	if _, ok := limiter.(*ratelimit.Redis); !ok {
		t.Errorf("newRateLimiter() type = %T, want *ratelimit.Redis", limiter)
	}

	allowed, err := limiter.Allow(context.Background())
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !allowed {
		t.Error("Allow() = false, want initial request allowed")
	}
	if wantKey := "prod:payments:ratelimit:users"; !server.Exists(wantKey) {
		t.Errorf("Redis key %q does not exist", wantKey)
	}
}

func TestNewRateLimiterRejectsRedisBackendWithoutClient(t *testing.T) {
	limiter, err := newRateLimiter(
		"users",
		router.RateLimitPolicy{
			Backend:           router.RateLimitBackendRedis,
			RequestsPerSecond: 2,
			Burst:             3,
		},
		nil,
		"devgate",
	)
	if err == nil {
		t.Fatal("newRateLimiter() error = nil, want missing Redis client error")
	}
	if limiter != nil {
		t.Errorf("newRateLimiter() limiter = %T, want nil", limiter)
	}
	if !strings.Contains(err.Error(), "redis client was not provided") {
		t.Errorf("newRateLimiter() error = %q, want Redis client context", err)
	}
}

func TestNewRateLimiterRejectsUnknownBackend(t *testing.T) {
	limiter, err := newRateLimiter(
		"users",
		router.RateLimitPolicy{
			Backend:           router.RateLimitBackend("memory"),
			RequestsPerSecond: 2,
			Burst:             3,
		},
		nil,
		"devgate",
	)
	if err == nil {
		t.Fatal("newRateLimiter() error = nil, want unknown backend error")
	}
	if limiter != nil {
		t.Errorf("newRateLimiter() limiter = %T, want nil", limiter)
	}
	if !strings.Contains(err.Error(), `unknown rate limiter backend "memory"`) {
		t.Errorf("newRateLimiter() error = %q, want backend value", err)
	}
}
