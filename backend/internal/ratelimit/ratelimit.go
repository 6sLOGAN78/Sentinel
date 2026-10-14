// Package ratelimit implements the Redis-backed event quota for public intake.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/6sLOGAN78/sentinel/internal/config"
)

const (
	scriptResultFields    = 3
	millisecondsPerSecond = int64(1000)
)

//nolint:gosec // Embedded Redis source contains no credential or secret.
const tokenBucketLua = `
local t = redis.call('TIME')
local now_ms = t[1] * 1000 + math.floor(t[2] / 1000)
local cap = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local d = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(d[1])
local ts = tonumber(d[2])
if tokens == nil or ts == nil then
  tokens = cap
  ts = now_ms
end
local elapsed = now_ms - ts
if elapsed < 0 then elapsed = 0 end
tokens = math.min(cap, tokens + elapsed * rate / 1000)
local allowed = 0
local retry_ms = 0
if cost > cap then
  retry_ms = -1
elseif tokens >= cost then
  tokens = tokens - cost
  allowed = 1
else
  retry_ms = math.ceil((cost - tokens) * 1000 / rate)
end
redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', now_ms)
redis.call('PEXPIRE', KEYS[1], math.ceil(cap * 1000 / rate) + 1000)
return {allowed, retry_ms, math.floor(tokens)}
`

var errInvalidInput = errors.New("rate limit key and cost must be valid")

// Limiter owns one script instance and one process-local fail-open counter.
type Limiter struct {
	client   redis.Scripter
	script   *redis.Script
	cfg      config.RateLimitConfig
	log      zerolog.Logger
	bypasses atomic.Uint64
}

// New constructs a limiter. The supplied client retains its configured AUTH,
// TLS, timeout, and retry policy.
func New(client redis.Scripter, cfg config.RateLimitConfig, log zerolog.Logger) *Limiter {
	return &Limiter{client: client, script: redis.NewScript(tokenBucketLua), cfg: cfg, log: log}
}

// Allow atomically charges cost events to the authenticated public key ID.
// retryAfter is an integer number of seconds suitable for Retry-After.
func (l *Limiter) Allow(ctx context.Context, keyID string, cost int) (bool, int, error) {
	if keyID == "" || cost <= 0 || cost > l.cfg.Capacity {
		return false, 0, errInvalidInput
	}

	callCtx, cancel := context.WithTimeout(ctx, l.cfg.Timeout)
	defer cancel()
	result, runErr := l.script.Run(
		callCtx, l.client, []string{"rl:" + keyID}, l.cfg.Capacity, l.cfg.RefillRate, cost,
	).Int64Slice()
	if runErr != nil {
		if l.cfg.FailMode == "open" {
			l.bypasses.Add(1)
			l.log.Warn().Str("event", "ratelimit_bypass").Msg("rate limit dependency unavailable")

			return true, 0, nil
		}

		return false, 0, fmt.Errorf("rate limit dependency unavailable: %w", runErr)
	}
	if len(result) != scriptResultFields {
		return false, 0, errors.New("rate limit script returned an invalid result")
	}
	if result[0] == 1 {
		return true, 0, nil
	}

	retryMillis := result[1]
	if retryMillis < 0 {
		return false, 0, errors.New("rate limit cost exceeds bucket capacity")
	}
	retryAfter := int((retryMillis + millisecondsPerSecond - 1) / millisecondsPerSecond)
	if retryAfter < 1 {
		retryAfter = 1
	}

	return false, retryAfter, nil
}

// BypassCount returns the number of requests admitted because explicit
// fail-open policy handled a Redis error.
func (l *Limiter) BypassCount() uint64 { return l.bypasses.Load() }
