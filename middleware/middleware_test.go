package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dr/server/log"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// umwSupabaseCapture records what the fake Supabase auth endpoint received so
// tests can assert the outgoing request headers parseToken builds.
type umwSupabaseCapture struct {
	path   string
	apiKey string
	auth   string
}

// umwStartSupabaseFake stands up an httptest server imitating the Supabase
// /auth/v1/user endpoint and points parseToken at it via t.Setenv (restored
// automatically). Returns the capture struct, populated after the first call.
func umwStartSupabaseFake(t *testing.T, status int, body string) *umwSupabaseCapture {
	t.Helper()
	captured := &umwSupabaseCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.path = r.URL.Path
		captured.apiKey = r.Header.Get("apikey")
		captured.auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("API_URL", srv.URL)
	t.Setenv("anon_key", "test-anon-key")
	return captured
}

// umwAuthEngine wires AuthMiddleware in front of a handler that echoes the
// resolved user_id, so tests can observe both rejection and success paths.
func umwAuthEngine() *gin.Engine {
	engine := gin.New()
	engine.Use(AuthMiddleware())
	engine.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetString("user_id")})
	})
	return engine
}

// umwQuietLogs raises the process minimum log level for the duration of the
// test so expected auth warnings/errors don't pollute output. LevelError+1
// silences everything, including error lines the test deliberately triggers.
func umwQuietLogs(t *testing.T, min log.Level) {
	t.Helper()
	restore := log.SetMinLevel(min)
	t.Cleanup(restore)
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	engine := umwAuthEngine()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"error":"Auth header missing"}`, w.Body.String())
}

func TestAuthMiddleware_HappyPath(t *testing.T) {
	captured := umwStartSupabaseFake(t, http.StatusOK,
		`{"id":"11111111-1111-1111-1111-111111111111","aud":"authenticated","role":"authenticated"}`)
	engine := umwAuthEngine()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer tok-123")
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"user_id":"11111111-1111-1111-1111-111111111111"}`, w.Body.String())

	// parseToken must call the Supabase user endpoint with the anon key and
	// re-attach the Bearer scheme to the trimmed incoming token (a failure to
	// trim would send "Bearer Bearer tok-123").
	assert.Equal(t, "/auth/v1/user", captured.path)
	assert.Equal(t, "test-anon-key", captured.apiKey)
	assert.Equal(t, "Bearer tok-123", captured.auth)
}

func TestAuthMiddleware_InvalidTokenNon200(t *testing.T) {
	umwQuietLogs(t, log.LevelError) // expected warn line for the 401 upstream
	umwStartSupabaseFake(t, http.StatusUnauthorized, `{"msg":"invalid JWT"}`)
	engine := umwAuthEngine()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"error":"Invalid token or user ID not found"}`, w.Body.String())
}

func TestAuthMiddleware_MalformedUserJSON(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1) // expected unmarshal error line
	umwStartSupabaseFake(t, http.StatusOK, `not-json`)
	engine := umwAuthEngine()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer tok")
	engine.ServeHTTP(w, req)

	// Unmarshal fails, user.ID stays empty → treated as invalid token.
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"error":"Invalid token or user ID not found"}`, w.Body.String())
}

// --- local JWT verification (parseToken's fast path) ---

const umwTestSecret = "super-secret-jwt-signing-key-for-tests"

// umwSignToken mints an HS256 token the way Supabase does, so tests can
// exercise verifyTokenLocally without a live project.
func umwSignToken(t *testing.T, secret string, claims jwt.Claims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

// umwUserClaims builds claims for a normal signed-in user.
func umwUserClaims(sub string, expiry time.Time) supabaseClaims {
	return supabaseClaims{
		Role: "authenticated",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			ExpiresAt: jwt.NewNumericDate(expiry),
		},
	}
}

func TestAuthMiddleware_LocalVerification_HappyPathSkipsNetwork(t *testing.T) {
	// Point the fallback at a fake server and assert it is never reached: with
	// a secret configured, authenticating must cost zero network round trips.
	captured := umwStartSupabaseFake(t, http.StatusOK, `{"id":"should-not-be-used"}`)
	t.Setenv("JWT_secret", umwTestSecret)

	const sub = "11111111-1111-1111-1111-111111111111"
	token := umwSignToken(t, umwTestSecret, umwUserClaims(sub, time.Now().Add(time.Hour)))

	engine := umwAuthEngine()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"user_id":"`+sub+`"}`, w.Body.String())
	assert.Empty(t, captured.path, "Supabase auth endpoint must not be called when the JWT secret is set")
}

func TestVerifyTokenLocally_Rejects(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1) // every case logs an expected warn line

	anonClaims := supabaseClaims{
		Role:             "anon",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}
	serviceClaims := supabaseClaims{
		Role:             "service_role",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}

	tests := []struct {
		name  string
		token string
	}{
		{"wrong signing secret", umwSignToken(t, "a-different-secret",
			umwUserClaims("11111111-1111-1111-1111-111111111111", time.Now().Add(time.Hour)))},
		{"expired token", umwSignToken(t, umwTestSecret,
			umwUserClaims("11111111-1111-1111-1111-111111111111", time.Now().Add(-time.Minute)))},
		// The anon key is public — it ships inside the mobile app — and is
		// signed with this same secret, so it must not authenticate anyone.
		{"anon api key", umwSignToken(t, umwTestSecret, anonClaims)},
		{"service_role api key", umwSignToken(t, umwTestSecret, serviceClaims)},
		{"no subject", umwSignToken(t, umwTestSecret, supabaseClaims{
			Role:             "authenticated",
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
		})},
		{"not a jwt", "garbage"},
		{"empty", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, verifyTokenLocally(tc.token, umwTestSecret))
		})
	}
}

// A token signed with "alg":"none" must not be accepted just because it parses.
func TestVerifyTokenLocally_RejectsUnsignedToken(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)

	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone,
		umwUserClaims("11111111-1111-1111-1111-111111111111", time.Now().Add(time.Hour)),
	).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	assert.Empty(t, verifyTokenLocally(unsigned, umwTestSecret))
}

// SUPABASE_JWT_SECRET is the documented name; JWT_secret is what the local
// .env already uses. Both must switch parseToken onto the local path.
func TestParseToken_EitherSecretNameEnablesLocalPath(t *testing.T) {
	const sub = "11111111-1111-1111-1111-111111111111"
	token := umwSignToken(t, umwTestSecret, umwUserClaims(sub, time.Now().Add(time.Hour)))

	for _, name := range []string{"SUPABASE_JWT_SECRET", "JWT_secret"} {
		t.Run(name, func(t *testing.T) {
			// Fallback would hit a dead port, so a non-empty result proves the
			// local path ran.
			t.Setenv("API_URL", "http://127.0.0.1:1")
			t.Setenv("SUPABASE_JWT_SECRET", "")
			t.Setenv("JWT_secret", "")
			t.Setenv(name, umwTestSecret)

			assert.Equal(t, sub, parseToken(token))
		})
	}
}

func TestParseToken_TransportError(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1) // expected connection-refused error line
	// Port 1 is essentially never listening; client.Do fails fast.
	t.Setenv("API_URL", "http://127.0.0.1:1")
	t.Setenv("anon_key", "test-anon-key")

	assert.Empty(t, parseToken("whatever"))
}

func TestAdminMiddleware_EchoesProvidedRequestID(t *testing.T) {
	engine := gin.New()
	engine.Use(AdminMiddleware())

	var ctxID, ginID string
	engine.GET("/x", func(c *gin.Context) {
		ginID = c.GetString("requestID")
		ctxID = GetRequestID(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "req-abc-123")
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "req-abc-123", w.Header().Get("X-Request-ID"), "header must echo the caller's ID")
	assert.Equal(t, "req-abc-123", ginID, "gin context must carry the ID")
	assert.Equal(t, "req-abc-123", ctxID, "request context must carry the ID for GetRequestID")
}

func TestAdminMiddleware_GeneratesRequestIDWithUserID(t *testing.T) {
	engine := gin.New()
	// A prior middleware (production: AuthMiddleware) sets user_id, exercising
	// the logger .With(user_id) branch.
	engine.Use(func(c *gin.Context) { c.Set("user_id", "11111111-1111-1111-1111-111111111111") })
	engine.Use(AdminMiddleware())

	var ctxID string
	engine.GET("/x", func(c *gin.Context) {
		ctxID = GetRequestID(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusOK, w.Code)
	headerID := w.Header().Get("X-Request-ID")
	_, err := uuid.Parse(headerID)
	assert.NoError(t, err, "generated request ID must be a UUID, got %q", headerID)
	assert.Equal(t, headerID, ctxID, "generated ID must propagate to the request context")
}

func TestRecovery_PanicReturns500(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1) // expected panic-recovered error line

	t.Run("recovery alone", func(t *testing.T) {
		engine := gin.New()
		engine.Use(Recovery())
		engine.GET("/boom", func(c *gin.Context) { panic("kaboom") })

		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("production order logger outer recovery inner", func(t *testing.T) {
		engine := gin.New()
		engine.Use(RequestLogger())
		engine.Use(Recovery())
		engine.GET("/boom", func(c *gin.Context) { panic(errors.New("kaboom")) })

		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// RequestLogger's log destination is os.Stderr with no injection seam, so
// these cases assert passthrough behavior (status and body unchanged) while
// driving every level branch; log content itself is not asserted.
func TestRequestLogger_StatusBranchesPassThrough(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)

	engine := gin.New()
	engine.Use(RequestLogger())
	engine.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok-body") })
	engine.GET("/bad", func(c *gin.Context) { c.String(http.StatusBadRequest, "bad-body") })
	engine.GET("/fail", func(c *gin.Context) { c.String(http.StatusInternalServerError, "fail-body") })
	engine.GET("/ginerr", func(c *gin.Context) {
		_ = c.Error(errors.New("attached error")) // gin_errors branch
		c.String(http.StatusOK, "err-body")
	})

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"info branch 200", "/ok", http.StatusOK, "ok-body"},
		{"warn branch 400", "/bad", http.StatusBadRequest, "bad-body"},
		{"error branch 500", "/fail", http.StatusInternalServerError, "fail-body"},
		{"gin errors attached", "/ginerr", http.StatusOK, "err-body"},
		{"unmatched 404 uses raw path", "/no/such/route", http.StatusNotFound, "404 page not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, tt.wantBody, w.Body.String())
		})
	}
}

func TestRequestLogger_WebsocketBranch(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)

	engine := gin.New()
	engine.Use(RequestLogger())
	engine.GET("/ws", func(c *gin.Context) { c.String(http.StatusOK, "ws-body") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")

	// The ws_connection branch must return without touching status attrs and
	// without disturbing the response.
	assert.NotPanics(t, func() { engine.ServeHTTP(w, req) })
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ws-body", w.Body.String())
}

func TestGetRequestID(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"present", context.WithValue(context.Background(), RequestIDKey, "req-1"), "req-1"},
		{"absent", context.Background(), "unknown"},
		{"wrong type", context.WithValue(context.Background(), RequestIDKey, 42), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetRequestID(tt.ctx))
		})
	}
}

// verifyTokenLocally attaches the token's subject to the rejection log line,
// but only when the failure is expiry — that is what makes an expired-token
// retry loop traceable to a user, since the 401 access line cannot carry a
// user_id (AuthMiddleware aborts before the middleware that resolves one).
//
// Its safety rests on an ordering guarantee inside golang-jwt: the parser
// verifies the signature and returns ErrTokenSignatureInvalid *before* it
// validates claims, so ErrTokenExpired can only surface for a token signed
// with our secret. That assumption lives in the library, not in our code, so
// pin it here. If an upgrade ever reorders the two, this test fails — and the
// subject logging becomes a way for anyone to write an arbitrary user_id into
// the logs rather than a diagnostic.
func TestJWTParse_ExpiryImpliesVerifiedSignature(t *testing.T) {
	const sub = "11111111-1111-1111-1111-111111111111"
	expired := time.Now().Add(-time.Minute)

	parse := func(token string) (supabaseClaims, error) {
		var claims supabaseClaims
		_, err := jwt.ParseWithClaims(token, &claims,
			func(*jwt.Token) (interface{}, error) { return []byte(umwTestSecret), nil },
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		)
		return claims, err
	}

	t.Run("expired but correctly signed exposes a trustworthy subject", func(t *testing.T) {
		claims, err := parse(umwSignToken(t, umwTestSecret, umwUserClaims(sub, expired)))
		require.Error(t, err)
		require.True(t, errors.Is(err, jwt.ErrTokenExpired),
			"expected ErrTokenExpired, got %v", err)
		assert.Equal(t, sub, claims.Subject,
			"claims must be populated for an expired token, or the log line carries nothing")
	})

	// The important half: claims from a token we did not sign are attacker
	// controlled, so they must never reach the log as a user_id.
	t.Run("wrong signature never reports expiry", func(t *testing.T) {
		_, err := parse(umwSignToken(t, "a-different-secret", umwUserClaims(sub, expired)))
		require.Error(t, err)
		assert.True(t, errors.Is(err, jwt.ErrTokenSignatureInvalid),
			"expected ErrTokenSignatureInvalid, got %v", err)
		assert.False(t, errors.Is(err, jwt.ErrTokenExpired),
			"signature failure must short-circuit before claims validation")
	})

	t.Run("garbage never reports expiry", func(t *testing.T) {
		_, err := parse("garbage")
		require.Error(t, err)
		assert.False(t, errors.Is(err, jwt.ErrTokenExpired))
	})
}
