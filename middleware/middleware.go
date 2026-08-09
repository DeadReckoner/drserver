package middleware

import (
	"context"
	"dr/server/log"
	"dr/server/observability"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
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

// envOnce guards loading .env. This used to run on every call to parseToken,
// which meant opening and parsing a file on the hot path of every
// authenticated request.
var envOnce sync.Once

// authHTTPClient is shared so TLS connections to Supabase are pooled across
// requests, and carries a timeout: the previous zero-value http.Client had
// none, so a Supabase auth endpoint that accepted the connection and then
// stalled would hang the request — and the user's app — indefinitely.
var authHTTPClient = &http.Client{Timeout: 10 * time.Second}

// supabaseClaims is the subset of a Supabase access token we act on.
type supabaseClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// supabaseJWTSecret returns the project's JWT signing secret, or empty when
// it isn't configured. SUPABASE_JWT_SECRET is the preferred name; JWT_secret
// matches the key already used in the local .env.
func supabaseJWTSecret() string {
	if s := os.Getenv("SUPABASE_JWT_SECRET"); s != "" {
		return s
	}
	return os.Getenv("JWT_secret")
}

// parseToken resolves an access token to a user id, or "" if the token isn't
// valid.
//
// Supabase signs access tokens with HS256 using the project's JWT secret, so
// when that secret is configured we verify the signature in-process and skip
// the network entirely. Without it we fall back to asking Supabase to validate
// the token — which is a full HTTPS round trip on *every* authenticated
// request, and was previously the only path.
//
// The fallback is what makes this safe to deploy before the secret is set:
// nothing changes until SUPABASE_JWT_SECRET lands in the environment.
func parseToken(accessToken string) string {
	envOnce.Do(func() { _ = godotenv.Load(".env") })

	if secret := supabaseJWTSecret(); secret != "" {
		return verifyTokenLocally(accessToken, secret)
	}
	return verifyTokenWithSupabase(accessToken)
}

// verifyTokenLocally checks the token's signature and expiry against the
// project secret and returns its subject.
//
// The role check is load-bearing: a Supabase project's anon and service_role
// API keys are themselves JWTs signed with this same secret. The anon key is
// public — it ships in the mobile app — so without requiring
// role == "authenticated" anyone could present it as a bearer token. It
// carries no subject, so the empty-sub check catches it too; both are here
// deliberately.
func verifyTokenLocally(accessToken, secret string) string {
	var claims supabaseClaims
	_, err := jwt.ParseWithClaims(accessToken, &claims,
		func(*jwt.Token) (interface{}, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	if err != nil {
		// Expired and malformed tokens are routine — clients retry with a
		// refreshed token — so this is a warning, not an error.
		authLog.Warn("supabase auth: token rejected", err.Error())
		return ""
	}

	if claims.Role != "authenticated" {
		authLog.Warn("supabase auth: token is not a user token", "role", claims.Role)
		return ""
	}
	if claims.Subject == "" {
		authLog.Warn("supabase auth: token has no subject")
		return ""
	}

	return claims.Subject
}

// verifyTokenWithSupabase asks the Supabase auth API to validate the token and
// returns the user id from the response. Used when no JWT secret is
// configured; see parseToken.
func verifyTokenWithSupabase(accessToken string) string {
	supabaseKey := os.Getenv("anon_key")
	supabaseApi := os.Getenv("API_URL")

	req, err := http.NewRequest("GET", supabaseApi+"/auth/v1/user", nil)
	if err != nil {
		authLog.Error("supabase auth: error creating request", err.Error())
		return ""
	}
	req.Header.Add("apiKey", supabaseKey)
	req.Header.Add("Authorization", "Bearer "+accessToken)

	resp, err := authHTTPClient.Do(req)
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
				stack := debug.Stack()
				log.FromContext(c.Request.Context()).LogAttrs(
					log.LevelError, "panic recovered",
					slog.String("panic", fmt.Sprintf("%v", r)),
					slog.String("stack", string(stack)),
				)

				// Also page it to Sentry. No-op when SENTRY_DSN is unset, so
				// local runs and tests are unaffected. The tags mirror the
				// fields on the access-log line so an issue can be traced back
				// to the matching request in Axiom.
				route := c.FullPath()
				if route == "" {
					route = c.Request.URL.Path
				}
				observability.ReportPanic(r, stack,
					c.GetString("requestID"), c.GetString("user_id"),
					c.Request.Method, route)

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
