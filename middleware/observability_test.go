package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dr/server/analytics"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func TestClientContext_ParsesHeaders(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set(HeaderPlatform, "ios")
	c.Request.Header.Set(HeaderAppVersion, "1.4.2")
	c.Request.Header.Set(HeaderAppBuild, "218")
	c.Request.Header.Set(HeaderAnonymousID, "anon-123")
	c.Request.Header.Set(HeaderSessionID, "sess-456")

	got := ClientContext(c)

	if got.Platform != "ios" || got.AppVersion != "1.4.2" || got.BuildNumber != "218" {
		t.Errorf("client metadata not parsed: %+v", got)
	}
	if got.AnonymousID != "anon-123" || got.SessionID != "sess-456" {
		t.Errorf("identity headers not parsed: %+v", got)
	}
}

func TestClientContext_AbsentHeadersAreEmpty(t *testing.T) {
	// Webhooks, health probes and curl carry no client headers. They must
	// produce empty values so they drop out of per-version aggregations rather
	// than piling into a blank bucket that looks like a real cohort.
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	if got := ClientContext(c); got != (ClientMeta{}) {
		t.Errorf("expected zero ClientMeta, got %+v", got)
	}
}

func TestSanitizeHeader_BoundsLengthAndStripsControlChars(t *testing.T) {
	// These values are attacker-controlled and land in the log aggregator as
	// fields, so an unbounded header is a way to write junk into the log
	// stream and a control character is a way to corrupt the JSON line.
	long := strings.Repeat("9", 500)
	if got := sanitizeHeader(long, 32); len(got) != 32 {
		t.Errorf("length = %d, want 32", len(got))
	}

	if got := sanitizeHeader("1.0\n\t\x00injected", 64); strings.ContainsAny(got, "\n\t\x00") {
		t.Errorf("control characters survived: %q", got)
	}

	if got := sanitizeHeader("  ios  ", 16); got != "ios" {
		t.Errorf("whitespace not trimmed: %q", got)
	}
}

func TestAdminMiddleware_PopulatesAnalyticsContext(t *testing.T) {
	var got analytics.RequestContext

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", "user-9")
		c.Next()
	})
	router.Use(AdminMiddleware())
	router.GET("/x", func(c *gin.Context) {
		got = analytics.RequestContextFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(HeaderPlatform, "android")
	req.Header.Set(HeaderAppVersion, "2.0.0")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if got.UserID != "user-9" {
		t.Errorf("user_id = %q, want user-9", got.UserID)
	}
	if got.RequestID == "" {
		t.Error("request_id must be populated so events correlate to access logs")
	}
	if got.Platform != "android" || got.AppVersion != "2.0.0" {
		t.Errorf("client metadata not attached: %+v", got)
	}
}

func TestDetachRequestContext_PreservesIdentityAcrossGoroutine(t *testing.T) {
	// Roadmap generation runs detached from the request; without this the
	// resulting events are anonymous and cannot be tied to the user who
	// triggered them.
	ctx := analytics.WithRequestContext(t.Context(), analytics.RequestContext{
		UserID:     "user-7",
		RequestID:  "req-7",
		Platform:   "ios",
		AppVersion: "3.1.0",
	})

	detached := DetachRequestContext(ctx, "job-1")

	rc := analytics.RequestContextFrom(detached)
	if rc.UserID != "user-7" || rc.Platform != "ios" || rc.AppVersion != "3.1.0" {
		t.Errorf("analytics identity lost on detach: %+v", rc)
	}
	if got := GetRequestID(detached); got != "req-7" {
		t.Errorf("request_id = %q, want req-7", got)
	}

	// It must not inherit cancellation from the request, or generation would
	// die the moment the response is written.
	if detached.Err() != nil {
		t.Errorf("detached context already carries an error: %v", detached.Err())
	}
}
