package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/redis/go-redis/v9"
)

func TestRoutesRequireRedis(t *testing.T) {
	tests := []struct {
		name   string
		routes []router.Route
		want   bool
	}{
		{
			name: "no routes",
		},
		{
			name: "route without rate limit",
			routes: []router.Route{
				{Name: "public"},
			},
		},
		{
			name: "local rate limit",
			routes: []router.Route{
				{
					Name: "local",
					RateLimit: &router.RateLimitPolicy{
						Backend: router.RateLimitBackendLocal,
					},
				},
			},
		},
		{
			name: "Redis rate limit",
			routes: []router.Route{
				{
					Name: "distributed",
					RateLimit: &router.RateLimitPolicy{
						Backend: router.RateLimitBackendRedis,
					},
				},
			},
			want: true,
		},
		{
			name: "mixed backends",
			routes: []router.Route{
				{
					Name: "local",
					RateLimit: &router.RateLimitPolicy{
						Backend: router.RateLimitBackendLocal,
					},
				},
				{
					Name: "distributed",
					RateLimit: &router.RateLimitPolicy{
						Backend: router.RateLimitBackendRedis,
					},
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := routesRequireRedis(test.routes); got != test.want {
				t.Errorf("routesRequireRedis() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestNewRedisClientConnectsWithConfiguredOptions(t *testing.T) {
	server := miniredis.RunT(t)
	server.RequireUserAuth("devgate", "test-password")
	cfg := config.RedisConfig{
		Addr:         server.Addr(),
		Username:     "devgate",
		Password:     "test-password",
		DB:           2,
		DialTimeout:  750 * time.Millisecond,
		ReadTimeout:  150 * time.Millisecond,
		WriteTimeout: 175 * time.Millisecond,
		KeyPrefix:    "test",
	}

	client, err := newRedisClient(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newRedisClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("newRedisClient() client = nil, want client")
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close Redis client: %v", closeErr)
		}
	})

	options := client.Options()
	if options.Addr != cfg.Addr {
		t.Errorf("Addr = %q, want %q", options.Addr, cfg.Addr)
	}
	if options.Username != cfg.Username {
		t.Errorf("Username = %q, want %q", options.Username, cfg.Username)
	}
	if options.Password != cfg.Password {
		t.Error("Password differs from configured value")
	}
	if options.DB != cfg.DB {
		t.Errorf("DB = %d, want %d", options.DB, cfg.DB)
	}
	if options.DialTimeout != cfg.DialTimeout {
		t.Errorf("DialTimeout = %s, want %s", options.DialTimeout, cfg.DialTimeout)
	}
	if options.ReadTimeout != cfg.ReadTimeout {
		t.Errorf("ReadTimeout = %s, want %s", options.ReadTimeout, cfg.ReadTimeout)
	}
	if options.WriteTimeout != cfg.WriteTimeout {
		t.Errorf("WriteTimeout = %s, want %s", options.WriteTimeout, cfg.WriteTimeout)
	}
	if options.MaxRetries != 0 {
		t.Errorf("normalized MaxRetries = %d, want 0 retries", options.MaxRetries)
	}
	if !options.ContextTimeoutEnabled {
		t.Error("ContextTimeoutEnabled = false, want true")
	}
	if pingErr := client.Ping(context.Background()).Err(); pingErr != nil {
		t.Errorf("Ping() after construction error = %v", pingErr)
	}
}

func TestNewRedisClientRejectsEmptyAddress(t *testing.T) {
	client, err := newRedisClient(context.Background(), config.RedisConfig{})
	if err == nil {
		t.Fatal("newRedisClient() error = nil, want empty address error")
	}
	if client != nil {
		t.Errorf("newRedisClient() client = %v, want nil", client)
	}
	if !strings.Contains(err.Error(), "redis address must not be empty") {
		t.Errorf("newRedisClient() error = %q, want empty address context", err)
	}
}

func TestNewRedisClientReturnsPingErrorWithoutLeakingPassword(t *testing.T) {
	server := miniredis.RunT(t)
	server.RequireUserAuth("devgate", "correct-password")
	const suppliedPassword = "wrong-secret-password"
	cfg := config.RedisConfig{
		Addr:         server.Addr(),
		Username:     "devgate",
		Password:     suppliedPassword,
		DialTimeout:  time.Second,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
		KeyPrefix:    "test",
	}

	client, err := newRedisClient(context.Background(), cfg)
	if err == nil {
		t.Fatal("newRedisClient() error = nil, want authentication error")
	}
	if client != nil {
		t.Errorf("newRedisClient() client = %v, want nil", client)
	}
	if !strings.Contains(err.Error(), "ping redis") {
		t.Errorf("newRedisClient() error = %q, want ping context", err)
	}
	if strings.Contains(err.Error(), suppliedPassword) {
		t.Error("newRedisClient() error contains Redis password")
	}
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
