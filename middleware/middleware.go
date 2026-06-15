package middleware

import (
	"context"
	"dr/server/log"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// setup context key type
type contextkey string

const RequestIDKey contextkey = "requestID"

type AuthenticatedUser struct {
	ID               string    `json:"id"`
	Aud              string    `json:"aud"`
	Role             string    `json:"role"`
	Email            string    `json:"email"`
	EmailConfirmedAt time.Time `json:"email_confirmed_at"`
	Phone            string    `json:"phone"`
	ConfirmedAt      time.Time `json:"confirmed_at"`
	LastSignInAt     time.Time `json:"last_sign_in_at"`
	AppMetadata      struct {
		Provider  string   `json:"provider"`
		Providers []string `json:"providers"`
	} `json:"app_metadata"`
	UserMetadata struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		PhoneVerified bool   `json:"phone_verified"`
		Sub           string `json:"sub"`
	} `json:"user_metadata"`
	Identities []struct {
		IdentityID   string `json:"identity_id"`
		ID           string `json:"id"`
		UserID       string `json:"user_id"`
		IdentityData struct {
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			PhoneVerified bool   `json:"phone_verified"`
			Sub           string `json:"sub"`
		} `json:"identity_data"`
		Provider     string    `json:"provider"`
		LastSignInAt time.Time `json:"last_sign_in_at"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
	} `json:"identities"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	IsAnonymous bool      `json:"is_anonymous"`
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Auth header missing"})
			c.Abort()
			return
		}

		accessToken := strings.TrimPrefix(authHeader, "Bearer ")
		user_id := parseToken(accessToken)

		if user_id == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token or user ID not found"})
			c.Abort()
			return
		}

		c.Set("user_id", user_id)
		c.Next()
	}

}

// curl -X GET 'http://127.0.0.1:54321/auth/v1/user' \
// -H "apikey: SUPABASE_KEY" \
// -H "Authorization: Bearer USER_TOKEN"

// authLog is the logger for the auth path, which runs outside any request
// context (parseToken fires before the request ID middleware can tag one).
var authLog = log.NewLogger("auth")

func parseToken(accessToken string) string {
	_ = godotenv.Load(".env")

	supabaseKey := os.Getenv("anon_key")
	supabaseApi := os.Getenv("API_URL")

	req, err := http.NewRequest("GET", supabaseApi+"/auth/v1/user", nil)
	if err != nil {
		authLog.Error("supabase auth: error creating request", err.Error())
		return ""
	}
	req.Header.Add("apiKey", supabaseKey)
	req.Header.Add("Authorization", "Bearer "+accessToken)

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		authLog.Error("supabase auth: error sending request", err.Error())
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		authLog.Error("supabase auth: error reading response body", err.Error())
		return ""
	}

	// The response body is the full Supabase user record — emails, identities,
	// sign-in history. That's PII, so it only ever logs at debug level.
	// Routine token rejections (expired/invalid) come back non-200 and are
	// worth a status-only line at warn.
	if resp.StatusCode != http.StatusOK {
		authLog.Warn("supabase auth: non-OK response", resp.Status)
	}
	authLog.Debug("supabase auth response", "status", resp.Status, "body", string(body))

	var user AuthenticatedUser
	if err := json.Unmarshal(body, &user); err != nil {
		authLog.Error("supabase auth: unable to unmarshal user response", err.Error())
	}
	return user.ID

}

func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		//Check if request header is preset else set the uuid
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		c.Set("requestID", requestID)

		logger := log.NewLogger(requestID)
		// AuthMiddleware runs before this on the /api/v1 group, so user_id is
		// already resolved — fold it into the request-scoped logger so every
		// downstream line (handlers, database layer) is tagged with it.
		if uid := c.GetString("user_id"); uid != "" {
			logger = logger.With(slog.String("user_id", uid))
		}

		ctx := context.WithValue(c.Request.Context(), RequestIDKey, requestID)
		ctx = log.WithLogger(ctx, logger)

		c.Request = c.Request.WithContext(ctx)

		// Set the header
		c.Header("X-Request-ID", requestID)

		c.Next()

	}
}

// RequestLogger emits one structured JSON line per HTTP request once it
// completes. These access events are what an aggregator (Axiom) builds API
// dashboards from: request counts per route, success/error rates (grouped by
// status), and latency (duration_ms). It reads user_id and request_id from the
// gin context, which the auth + request-context middleware populate on
// /api/v1 routes. Replaces gin's default text access logger.
//
// Registered OUTER of Recovery so that panic-induced 500s — which Recovery
// turns into a normal 500 response before this handler's post-Next code runs —
// still produce an access line with the right status.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		isWebsocket := c.IsWebsocket()

		c.Next()

		// Prefer the matched route template (e.g. /api/v1/roadmaps/:roadmapid)
		// so per-endpoint aggregation groups by route rather than by the
		// thousands of distinct ids that appear in the concrete path. Falls
		// back to the raw path for unmatched (404) requests.
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.String("path", c.Request.URL.Path),
			slog.String("client_ip", c.ClientIP()),
		}
		if uid := c.GetString("user_id"); uid != "" {
			attrs = append(attrs, slog.String("user_id", uid))
		}
		logger := log.NewLogger(c.GetString("requestID"))

		// WebSocket upgrades hijack the response writer and are long-lived, so
		// their post-handler status/size are meaningless and their duration is
		// the whole session — logging that as a normal "request" would wreck
		// latency percentiles. Emit a distinct event (excluded from request
		// metrics, which filter on msg == "request") with the session length
		// under its own field.
		if isWebsocket {
			attrs = append(attrs, slog.Int64("session_ms", time.Since(start).Milliseconds()))
			logger.LogAttrs(log.LevelInfo, "ws_connection", attrs...)
			return
		}

		status := c.Writer.Status()
		attrs = append(attrs,
			slog.Int("status", status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.Int("bytes", c.Writer.Size()),
		)
		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("gin_errors", c.Errors.String()))
		}

		switch {
		case status >= 500:
			logger.LogAttrs(log.LevelError, "request", attrs...)
		case status >= 400:
			logger.LogAttrs(log.LevelWarn, "request", attrs...)
		default:
			logger.LogAttrs(log.LevelInfo, "request", attrs...)
		}
	}
}

// Recovery catches panics from downstream handlers and logs them as a
// structured JSON line — with request_id/user_id from context — instead of
// gin.Recovery's ANSI-colored text dump (which would be non-JSON noise in the
// log stream). It then returns 500. Registered INNER of RequestLogger so the
// recovered request still gets an access line with status 500.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.FromContext(c.Request.Context()).LogAttrs(
					log.LevelError, "panic recovered",
					slog.String("panic", fmt.Sprintf("%v", r)),
					slog.String("stack", string(debug.Stack())),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

func GetRequestID(ctx context.Context) string { // Exported helper
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return "unknown"
}
