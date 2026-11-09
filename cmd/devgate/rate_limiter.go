package main

import (
	"errors"
	"fmt"

	"github.com/chyioishi/devgate/internal/ratelimit"
	"github.com/chyioishi/devgate/internal/router"
	"github.com/redis/go-redis/v9"
)

func newRateLimiter(
	routeName string,
	policy router.RateLimitPolicy,
	redisClient *redis.Client,
	redisKeyPrefix string,
) (ratelimit.Limiter, error) {
	switch policy.Backend {
	case router.RateLimitBackendLocal:
		return ratelimit.NewLocal(
			policy.RequestsPerSecond,
			policy.Burst,
		)
	case router.RateLimitBackendRedis:
		if redisClient == nil {
			return nil, errors.New("redis client was not provided")
		}
		redisKey := fmt.Sprintf(
			"%s:ratelimit:%s",
			redisKeyPrefix,
			routeName,
		)
		return ratelimit.NewRedis(
			redisClient,
			redisKey,
			policy.RequestsPerSecond,
			policy.Burst,
		)
	default:
		return nil, fmt.Errorf(
			"unknown rate limiter backend %q",
			policy.Backend,
		)
	}
}
