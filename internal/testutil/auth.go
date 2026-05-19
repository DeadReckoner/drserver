package testutil

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// TestUserIDHeader is the request header the fake auth middleware reads to
// determine the authenticated user. Tests set this header on each request
// instead of producing real Supabase JWTs.
const TestUserIDHeader = "X-Test-User-ID"

// FakeAuthMiddleware returns a gin middleware that mimics the production
// AuthMiddleware contract (sets "user_id" on the context) by reading the
// TestUserIDHeader. Requests without the header receive 401, matching the
// production behavior for a missing Authorization header.
func FakeAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetHeader(TestUserIDHeader)
		if userID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Auth header missing"})
			c.Abort()
			return
		}
		c.Set("user_id", userID)
		c.Next()
	}
}

// FakeAuthMiddlewareAs returns a middleware that always authenticates as the
// given userID, ignoring request headers. Useful when a test only exercises
// one user and you want to reduce per-request boilerplate.
func FakeAuthMiddlewareAs(userID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	}
}
