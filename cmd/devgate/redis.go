package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/redis/go-redis/v9"
)

func newRedisClient(
	ctx context.Context,
	cfg config.RedisConfig,
) (*redis.Client, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, errors.New("redis address must not be empty")
	}
	client := redis.NewClient(
		&redis.Options{
			Addr:                  cfg.Addr,
			Username:              cfg.Username,
			Password:              cfg.Password,
			DB:                    cfg.DB,
			DialTimeout:           cfg.DialTimeout,
			ReadTimeout:           cfg.ReadTimeout,
			WriteTimeout:          cfg.WriteTimeout,
			MaxRetries:            -1,
			ContextTimeoutEnabled: true,
		},
	)
	if err := client.Ping(ctx).Err(); err != nil {
		pingErr := fmt.Errorf(
			"ping redis addr %q: %w",
			cfg.Addr,
			err,
		)
		if closeErr := client.Close(); closeErr != nil {
			return nil, errors.Join(
				pingErr,
				fmt.Errorf(
					"close redis client after failed ping: %w",
					closeErr,
				),
			)
		}
		return nil, pingErr
	}
	return client, nil
}

func routesRequireRedis(routes []router.Route) bool {
	for _, route := range routes {
		if route.RateLimit != nil {
			if route.RateLimit.Backend == router.RateLimitBackendRedis {
				return true
			}
		}
	}
	return false
}
