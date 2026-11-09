package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/redis/go-redis/v9"
)

var redisTokenBucketScript = redis.NewScript(`
	local key = KEYS[1]
	local rate = tonumber(ARGV[1])
	local burst = tonumber(ARGV[2])
	local ttl = tonumber(ARGV[3])
	local current_time = redis.call('TIME')
	local now = tonumber(current_time[1]) + tonumber(current_time[2]) / 1000000

	local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')

	local tokens = bucket[1] and tonumber(bucket[1]) or burst
	local last_refill = bucket[2] and tonumber(bucket[2]) or now

	local elapsed = math.max(0, now - last_refill)
	tokens = math.min(burst, tokens + elapsed * rate)

	local allowed = 0
	if tokens >= 1 then
		tokens = tokens - 1
		allowed = 1
	end

	redis.call(
		'HSET',
		key,
		'tokens', tokens,
		'last_refill', now
	)
	redis.call('PEXPIRE', key, ttl)

	return allowed
`)

// Redis is a distributed token-bucket rate limiter backed by Redis.
// It does not own the Redis client and therefore does not close it.
type Redis struct {
	client            *redis.Client
	key               string
	requestsPerSecond float64
	burst             int
	ttlMilliseconds   int64
}

var _ Limiter = (*Redis)(nil)

// NewRedis creates a Redis-backed rate limiter for key.
func NewRedis(
	client *redis.Client,
	key string,
	requestsPerSecond float64,
	burst int,
) (*Redis, error) {
	if client == nil {
		return nil, errors.New("redis client was not provided")
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("key must not be empty")
	}
	if requestsPerSecond <= 0 {
		return nil, errors.New("requests per second limit must be positive")
	}
	if math.IsNaN(requestsPerSecond) || math.IsInf(requestsPerSecond, 0) {
		return nil, errors.New("requests per second limit must be in a finite range")
	}
	if burst <= 0 {
		return nil, errors.New("burst must be positive")
	}
	ttl := math.Ceil(float64(burst) / requestsPerSecond * 1000)
	if math.IsInf(ttl, 1) || ttl >= float64(math.MaxInt64) {
		return nil, errors.New("token bucket refill duration is too large")
	}
	return &Redis{
		client:            client,
		key:               key,
		requestsPerSecond: requestsPerSecond,
		burst:             burst,
		ttlMilliseconds:   int64(ttl),
	}, nil
}

// Allow atomically consumes one token from the shared Redis bucket and reports
// whether the request may proceed.
func (l *Redis) Allow(ctx context.Context) (bool, error) {
	result := redisTokenBucketScript.Run(
		ctx,
		l.client,
		[]string{l.key},
		l.requestsPerSecond,
		l.burst,
		l.ttlMilliseconds,
	)

	allowed, err := result.Int64()
	if err != nil {
		return false, fmt.Errorf("redis allow request: %w", err)
	}
	switch allowed {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf(
			"unexpected script result: %d",
			allowed,
		)
	}
}
