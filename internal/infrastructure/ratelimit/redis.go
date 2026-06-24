package ratelimit

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/redis/go-redis/v9"
)

const defaultRedisRateLimitPrefix = "soha:ai-gateway:rate-limit"

var redisGCRAScript = redis.NewScript(`
local tat = tonumber(redis.call("GET", KEYS[1]) or "0")
local now_at = tonumber(ARGV[1])
local interval = tonumber(ARGV[2])
local burst = tonumber(ARGV[3])
local ttl_seconds = tonumber(ARGV[4])
local current_tat = tat
if current_tat < now_at then
  current_tat = now_at
end
local allowed_at = tat - ((burst - 1) * interval)
if tat == 0 or allowed_at <= now_at then
  local new_tat = current_tat + interval
  redis.call("SET", KEYS[1], new_tat, "EX", ttl_seconds)
  return {1, new_tat, 0}
end
redis.call("EXPIRE", KEYS[1], ttl_seconds)
return {0, tat, allowed_at - now_at}
`)

type RedisBackend struct {
	client  *redis.Client
	prefix  string
	timeout time.Duration
}

func NewRedisBackend(cfg cfgpkg.AIGatewayRateLimitConfig) (*RedisBackend, error) {
	addr := strings.TrimSpace(cfg.Redis.Addr)
	if addr == "" {
		return nil, fmt.Errorf("redis rate-limit backend requires ai_gateway.rate_limit.redis.addr")
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Redis.KeyPrefix), ":")
	if prefix == "" {
		prefix = defaultRedisRateLimitPrefix
	}
	timeout := cfg.Redis.Timeout
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	options := &redis.Options{
		Addr:         addr,
		Username:     strings.TrimSpace(cfg.Redis.Username),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  timeout,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
	}
	if cfg.Redis.TLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &RedisBackend{
		client:  redis.NewClient(options),
		prefix:  prefix,
		timeout: timeout,
	}, nil
}

func (b *RedisBackend) Close() error {
	if b == nil || b.client == nil {
		return nil
	}
	return b.client.Close()
}

func (b *RedisBackend) IncrementRateLimitCounter(ctx context.Context, item domainaigateway.RateLimitCounter) (domainaigateway.RateLimitCounter, error) {
	if b == nil || b.client == nil {
		return item, nil
	}
	ctx, cancel := b.withTimeout(ctx)
	defer cancel()

	now := time.Now().UTC()
	key := b.key("counter", item.Key)
	expireAt := item.WindowEnd.UTC().Add(time.Minute)
	if !expireAt.After(now) {
		expireAt = now.Add(time.Minute)
	}
	var countCmd *redis.IntCmd
	_, err := b.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		countCmd = pipe.Incr(ctx, key)
		pipe.ExpireAt(ctx, key, expireAt)
		return nil
	})
	if err != nil {
		return domainaigateway.RateLimitCounter{}, err
	}
	item.Count = int(countCmd.Val())
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return item, nil
}

func (b *RedisBackend) ApplyRateLimitState(ctx context.Context, item domainaigateway.RateLimitState) (domainaigateway.RateLimitState, error) {
	if b == nil || b.client == nil {
		return item, nil
	}
	ctx, cancel := b.withTimeout(ctx)
	defer cancel()

	now := time.Now().UTC()
	burst := item.Burst
	if burst <= 0 {
		burst = 1
	}
	intervalMicros := int64(math.Round(item.IntervalSeconds * float64(time.Second/time.Microsecond)))
	if intervalMicros <= 0 {
		intervalMicros = int64(time.Millisecond / time.Microsecond)
	}
	ttlSeconds := int64(math.Ceil(item.IntervalSeconds * float64(max(item.Limit, 1)+burst) * 2))
	if ttlSeconds < 60 {
		ttlSeconds = 60
	}

	result, err := redisGCRAScript.Run(ctx, b.client, []string{b.key("gcra", item.Key)}, now.UnixMicro(), intervalMicros, burst, ttlSeconds).Result()
	if err != nil {
		return domainaigateway.RateLimitState{}, err
	}
	values, ok := result.([]any)
	if !ok || len(values) < 3 {
		return domainaigateway.RateLimitState{}, fmt.Errorf("unexpected redis GCRA result: %#v", result)
	}
	allowed, err := redisResultInt64(values[0])
	if err != nil {
		return domainaigateway.RateLimitState{}, err
	}
	tatMicros, err := redisResultInt64(values[1])
	if err != nil {
		return domainaigateway.RateLimitState{}, err
	}
	retryMicros, err := redisResultInt64(values[2])
	if err != nil {
		return domainaigateway.RateLimitState{}, err
	}

	item.Burst = burst
	item.Allowed = allowed == 1
	item.TAT = time.UnixMicro(tatMicros).UTC()
	item.RetryAfter = time.Duration(retryMicros) * time.Microsecond
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return item, nil
}

func (b *RedisBackend) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if b.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, b.timeout)
}

func (b *RedisBackend) key(kind, raw string) string {
	return b.prefix + ":" + strings.Trim(kind, ":") + ":" + strings.TrimSpace(raw)
}

func redisResultInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case uint64:
		return int64(typed), nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis integer result %T", value)
	}
}
