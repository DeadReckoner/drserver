package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newTestLimiter builds a limiter with a controllable clock so tests can move
// time without sleeping.
func newTestLimiter(limit int, window time.Duration, clock *time.Time) *RateLimiter {
	rl := NewRateLimiter(limit, window)
	rl.now = func() time.Time { return *clock }
	return rl
}

func TestRateLimiter_AllowsUpToLimitThenBlocks(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rl := newTestLimiter(3, time.Minute, &now)

	for i := 1; i <= 3; i++ {
		if ok, _ := rl.Allow("user-1"); !ok {
			t.Fatalf("request %d denied, want allowed within limit of 3", i)
		}
	}
	ok, retryAfter := rl.Allow("user-1")
	if ok {
		t.Fatal("4th request allowed, want denied")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want a positive wait", retryAfter)
	}
}

func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rl := newTestLimiter(1, time.Minute, &now)

	if ok, _ := rl.Allow("user-1"); !ok {
		t.Fatal("user-1 first request denied")
	}
	if ok, _ := rl.Allow("user-1"); ok {
		t.Fatal("user-1 second request allowed, want denied")
	}
	// One user exhausting their bucket must not affect anyone else.
	if ok, _ := rl.Allow("user-2"); !ok {
		t.Error("user-2 denied because user-1 was rate limited")
	}
}

func TestRateLimiter_RefillsGraduallyNotAtWindowBoundary(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rl := newTestLimiter(6, time.Minute, &now) // one token per 10s

	for i := 0; i < 6; i++ {
		rl.Allow("user-1")
	}
	if ok, _ := rl.Allow("user-1"); ok {
		t.Fatal("request allowed with an empty bucket")
	}

	// Ten seconds buys exactly one token back — not the whole window's worth.
	now = now.Add(10 * time.Second)
	if ok, _ := rl.Allow("user-1"); !ok {
		t.Error("no token after a full refill interval elapsed")
	}
	if ok, _ := rl.Allow("user-1"); ok {
		t.Error("two tokens granted for one refill interval; refill is not gradual")
	}
}

func TestRateLimiter_RefillCapsAtLimit(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rl := newTestLimiter(2, time.Minute, &now)

	rl.Allow("user-1")
	// Idling for many windows must not bank tokens beyond capacity.
	now = now.Add(10 * time.Minute)

	if ok, _ := rl.Allow("user-1"); !ok {
		t.Fatal("first request after idle denied")
	}
	if ok, _ := rl.Allow("user-1"); !ok {
		t.Fatal("second request after idle denied")
	}
	if ok, _ := rl.Allow("user-1"); ok {
		t.Error("third request allowed; bucket refilled past its capacity")
	}
}

func TestRateLimiter_EvictsIdleBuckets(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rl := newTestLimiter(3, time.Minute, &now)

	rl.Allow("user-1")
	if len(rl.buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(rl.buckets))
	}

	now = now.Add(2 * time.Minute)
	rl.evictIdle()
	if len(rl.buckets) != 0 {
		t.Errorf("buckets = %d after idle sweep, want 0 — memory would grow "+
			"unbounded with one entry per user seen", len(rl.buckets))
	}
}

func TestRateLimiter_ActiveBucketsSurviveEviction(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	rl := newTestLimiter(3, time.Minute, &now)

	rl.Allow("user-1")
	now = now.Add(10 * time.Second)
	rl.evictIdle()

	if len(rl.buckets) != 1 {
		t.Error("recently-active bucket evicted; a user could reset their own " +
			"limit by pausing briefly")
	}
}

// --- middleware behavior ------------------------------------------------------

func runAIRateLimit(t *testing.T, userID string, n int) []int {
	t.Helper()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != "" {
			c.Set("user_id", userID)
		}
		c.Next()
	})
	r.POST("/generate", AIRateLimit(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	codes := make([]int, n)
	for i := range codes {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/generate", nil))
		codes[i] = w.Code
	}
	return codes
}

func TestAIRateLimit_Returns429WithRetryAfterOnceBurstExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "user-1"); c.Next() })
	r.POST("/generate", AIRateLimit(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	var last *httptest.ResponseRecorder
	for i := 0; i < defaultAIBurstLimit+1; i++ {
		last = httptest.NewRecorder()
		r.ServeHTTP(last, httptest.NewRequest(http.MethodPost, "/generate", nil))
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after exceeding the burst limit", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After header; clients need it to back off correctly")
	}
}

// Auth runs before this middleware, so a missing user_id means the request was
// never going to reach a handler. Keying those under "" would let one bad
// actor exhaust a shared bucket.
func TestAIRateLimit_PassesThroughWhenUnauthenticated(t *testing.T) {
	codes := runAIRateLimit(t, "", defaultAIBurstLimit+3)
	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200: unauthenticated requests must "+
				"not share a rate-limit bucket", i+1, code)
		}
	}
}

func TestAIRateLimit_LimitsAreSharedAcrossBilledEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// One AIRateLimit() instance across a group, mirroring how routes.go wires
	// it: a user shouldn't get a fresh budget per endpoint.
	limiter := AIRateLimit()
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "user-1"); c.Next() })
	g := r.Group("", limiter)
	handler := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	g.POST("/generate", handler)
	g.POST("/import-plan", handler)

	paths := []string{"/generate", "/import-plan", "/generate", "/import-plan"}
	var codes []int
	for _, p := range paths {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, p, nil))
		codes = append(codes, w.Code)
	}

	if codes[len(codes)-1] != http.StatusTooManyRequests {
		t.Errorf("codes = %v; want the 4th call limited — spreading calls "+
			"across endpoints must not multiply the budget", codes)
	}
}
