package middleware

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Abuse limits for the AI endpoints. These are a spend guard, NOT a business
// quota — the plan limits in database/plan_check.go own the product rules.
//
// The distinction matters because the plan layer does not actually bound spend:
//
//   - it no-ops entirely unless PRO_GATING_ENABLED is set, which defaults off
//   - paid users are exempt from the AI cap (plan_check.go returns early on
//     IsPaid), so a paid account has no ceiling at all
//   - the counter increments on successful completion, minutes after admission,
//     so N concurrent requests all pass the check before any of them count
//
// Any one of those leaves a path to unbounded provider spend from a single
// account — a retry loop, a leaked token, a stuck client. These limits close
// that path for every user regardless of plan, and are set well above real
// usage: a roadmap generation takes minutes, so nobody legitimately starts
// four in a minute.
const (
	defaultAIBurstLimit  = 3
	defaultAIBurstWindow = time.Minute
	defaultAIDailyLimit  = 40
	defaultAIDailyWindow = 24 * time.Hour
)

// aiRateLimitEnv reads an int from the environment, falling back to def when
// unset or unparseable. Lets us tighten limits in production without a deploy.
func aiRateLimitEnv(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

// bucket is one user's token bucket for one window. Tokens refill linearly
// over refill, so a user who spends their burst gets one back every
// refill/limit rather than all of them at the window boundary — that avoids
// the fixed-window edge where a client can fire 2x the limit across a
// boundary.
type bucket struct {
	tokens float64
	last   time.Time
}

// RateLimiter is a per-key token bucket with a background eviction sweep.
//
// State is per-process and in-memory. With more than one machine the effective
// limit is (limit x machines), and a deploy resets every bucket. That is an
// accepted trade for an abuse guard: it still bounds the blast radius of a
// runaway client by orders of magnitude without putting a write on the hot
// path of every request. If we ever need exact global limits, this is the
// seam to move behind Redis or a Postgres counter.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	limit  float64
	window time.Duration
	now    func() time.Time
}

// NewRateLimiter builds a limiter allowing limit events per window per key.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		limit:   float64(limit),
		window:  window,
		now:     time.Now,
	}
}

// Allow consumes a token for key, reporting whether the request may proceed
// and — when it may not — how long until a token is available.
func (rl *RateLimiter) Allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	b, ok := rl.buckets[key]
	if !ok {
		// First request for this key starts full, minus the one it spends.
		rl.buckets[key] = &bucket{tokens: rl.limit - 1, last: now}
		return true, 0
	}

	// Refill for elapsed time, capped at the bucket's capacity.
	refillPerSecond := rl.limit / rl.window.Seconds()
	b.tokens += now.Sub(b.last).Seconds() * refillPerSecond
	if b.tokens > rl.limit {
		b.tokens = rl.limit
	}
	b.last = now

	if b.tokens < 1 {
		deficit := 1 - b.tokens
		return false, time.Duration(deficit / refillPerSecond * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// evictIdle drops buckets that have fully refilled, since a full bucket is
// indistinguishable from a key we've never seen. Bounds memory to keys active
// within the last window.
func (rl *RateLimiter) evictIdle() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	for key, b := range rl.buckets {
		if now.Sub(b.last) >= rl.window {
			delete(rl.buckets, key)
		}
	}
}

// StartEvictionSweep runs evictIdle on a ticker until ctx-less shutdown. The
// goroutine lives for the process lifetime by design — one per limiter, of
// which there are two.
func (rl *RateLimiter) StartEvictionSweep(every time.Duration) {
	go func() {
		for range time.Tick(every) {
			rl.evictIdle()
		}
	}()
}

// AIRateLimit builds the middleware guarding the AI endpoints. It stacks a
// short burst window against a long daily window so a client that paces itself
// under the burst limit still can't run all day.
//
// Keyed on user_id, so it must be registered after AuthMiddleware. An
// unauthenticated request (no user_id in context) is passed through untouched
// — auth has already rejected it, and keying every anonymous request under ""
// would make one bad actor lock out the bucket for all of them.
func AIRateLimit() gin.HandlerFunc {
	burst := NewRateLimiter(
		aiRateLimitEnv("AI_BURST_LIMIT", defaultAIBurstLimit),
		defaultAIBurstWindow,
	)
	daily := NewRateLimiter(
		aiRateLimitEnv("AI_DAILY_LIMIT", defaultAIDailyLimit),
		defaultAIDailyWindow,
	)

	// Sweep on the burst window; the daily limiter's own entries age out on
	// the same pass once they go idle for a full day.
	burst.StartEvictionSweep(defaultAIBurstWindow)
	daily.StartEvictionSweep(time.Hour)

	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		if userID == "" {
			c.Next()
			return
		}

		if ok, retryAfter := burst.Allow(userID); !ok {
			abortRateLimited(c, retryAfter,
				"You're starting generations too quickly. Please wait a moment and try again.")
			return
		}
		if ok, retryAfter := daily.Allow(userID); !ok {
			abortRateLimited(c, retryAfter,
				"You've hit today's limit for AI generations. Please try again tomorrow.")
			return
		}

		c.Next()
	}
}

// abortRateLimited writes a 429 with a Retry-After header. The message is
// user-facing: the mobile client surfaces it directly.
func abortRateLimited(c *gin.Context, retryAfter time.Duration, msg string) {
	seconds := int(retryAfter.Seconds()) + 1
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":       msg,
		"code":        "rate_limited",
		"retry_after": seconds,
	})
}
