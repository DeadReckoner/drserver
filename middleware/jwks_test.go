package middleware

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"dr/server/log"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jwksTestSub = "11111111-1111-1111-1111-111111111111"

// jwksFake imitates the two Supabase auth endpoints parseToken can reach:
// the published key set and the /user validation endpoint. It counts calls
// to each so tests can assert which path ran.
type jwksFake struct {
	mu         sync.Mutex
	keys       []map[string]string
	jwksStatus int
	jwksCalls  int
	userCalls  int
	userStatus int
	userBody   string
}

func (f *jwksFake) setKeys(keys ...map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = keys
}

func (f *jwksFake) counts() (jwks, user int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jwksCalls, f.userCalls
}

// jwksStartFake starts the fake, points API_URL at it, and resets the
// process-wide key cache before and after so tests cannot see each other's
// keys.
func jwksStartFake(t *testing.T, keys ...map[string]string) *jwksFake {
	t.Helper()
	f := &jwksFake{keys: keys, jwksStatus: http.StatusOK, userStatus: http.StatusUnauthorized, userBody: `{"msg":"invalid JWT"}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case jwksPath:
			f.jwksCalls++
			w.WriteHeader(f.jwksStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": f.keys})
		case "/auth/v1/user":
			f.userCalls++
			w.WriteHeader(f.userStatus)
			_, _ = w.Write([]byte(f.userBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	jwksResetCache(t)
	t.Setenv("API_URL", srv.URL)
	t.Setenv("anon_key", "test-anon-key")
	t.Setenv("SUPABASE_JWT_SECRET", "")
	t.Setenv("JWT_secret", "")
	return f
}

func jwksResetCache(t *testing.T) {
	t.Helper()
	reset := func() {
		supabaseJWKS.mu.Lock()
		defer supabaseJWKS.mu.Unlock()
		supabaseJWKS.url, supabaseJWKS.keys = "", nil
		supabaseJWKS.fetchedAt, supabaseJWKS.lastAttempt = time.Time{}, time.Time{}
	}
	reset()
	t.Cleanup(reset)
}

// jwksSetRefetchInterval overrides the fetch rate limit for one test.
func jwksSetRefetchInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := jwksMinRefetch
	jwksMinRefetch = d
	t.Cleanup(func() { jwksMinRefetch = prev })
}

func jwksNewECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

// jwksECPublic renders an EC public key the way Supabase publishes it.
func jwksECPublic(t *testing.T, kid string, pub *ecdsa.PublicKey) map[string]string {
	t.Helper()
	raw, err := pub.Bytes() // 0x04 || X || Y
	require.NoError(t, err)
	require.Len(t, raw, 65)
	enc := base64.RawURLEncoding.EncodeToString
	return map[string]string{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
		"x": enc(raw[1:33]), "y": enc(raw[33:]),
	}
}

func jwksSign(t *testing.T, method jwt.SigningMethod, kid string, key any, claims jwt.Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	signed, err := tok.SignedString(key)
	require.NoError(t, err)
	return signed
}

func TestParseToken_ES256_VerifiesLocallyAndCachesKeys(t *testing.T) {
	key := jwksNewECKey(t)
	fake := jwksStartFake(t, jwksECPublic(t, "kid-1", &key.PublicKey))
	token := jwksSign(t, jwt.SigningMethodES256, "kid-1", key, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Equal(t, jwksTestSub, parseToken(token))
	assert.Equal(t, jwksTestSub, parseToken(token))

	jwks, user := fake.counts()
	assert.Equal(t, 1, jwks, "the key set must be fetched once and then served from cache")
	assert.Zero(t, user, "a locally verified token must not be sent to Supabase")
}

func TestAuthMiddleware_ES256_HappyPath(t *testing.T) {
	key := jwksNewECKey(t)
	jwksStartFake(t, jwksECPublic(t, "kid-1", &key.PublicKey))
	token := jwksSign(t, jwt.SigningMethodES256, "kid-1", key, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	engine := umwAuthEngine()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"user_id":"`+jwksTestSub+`"}`, w.Body.String())
}

// A project mid-migration issues ES256 tokens while HS256 tokens from before
// the switch are still live, with the legacy secret still configured. Both
// must authenticate, and neither may need the network fallback.
func TestParseToken_ES256AndHS256SideBySide(t *testing.T) {
	key := jwksNewECKey(t)
	fake := jwksStartFake(t, jwksECPublic(t, "kid-1", &key.PublicKey))
	t.Setenv("SUPABASE_JWT_SECRET", umwTestSecret)

	es := jwksSign(t, jwt.SigningMethodES256, "kid-1", key, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))
	hs := umwSignToken(t, umwTestSecret, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Equal(t, jwksTestSub, parseToken(es))
	assert.Equal(t, jwksTestSub, parseToken(hs))

	_, user := fake.counts()
	assert.Zero(t, user)
}

func TestParseToken_ES256_Rejects(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1) // every case logs an expected warn line

	key := jwksNewECKey(t)
	other := jwksNewECKey(t)
	future := time.Now().Add(time.Hour)
	roleClaims := func(role string) supabaseClaims {
		return supabaseClaims{Role: role, RegisteredClaims: jwt.RegisteredClaims{
			Subject: jwksTestSub, ExpiresAt: jwt.NewNumericDate(future),
		}}
	}

	tests := []struct {
		name  string
		token string
	}{
		// Right kid, wrong private key: the signature must be what decides.
		{"signed by a different key", jwksSign(t, jwt.SigningMethodES256, "kid-1", other, umwUserClaims(jwksTestSub, future))},
		{"expired", jwksSign(t, jwt.SigningMethodES256, "kid-1", key, umwUserClaims(jwksTestSub, time.Now().Add(-time.Minute)))},
		{"anon role", jwksSign(t, jwt.SigningMethodES256, "kid-1", key, roleClaims("anon"))},
		{"service_role", jwksSign(t, jwt.SigningMethodES256, "kid-1", key, roleClaims("service_role"))},
		{"no subject", jwksSign(t, jwt.SigningMethodES256, "kid-1", key, supabaseClaims{
			Role:             "authenticated",
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(future)},
		})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := jwksStartFake(t, jwksECPublic(t, "kid-1", &key.PublicKey))
			// If a rejected token leaked through to the fallback, this would
			// authenticate it.
			fake.userStatus, fake.userBody = http.StatusOK, `{"id":"should-not-be-used"}`

			assert.Empty(t, parseToken(tc.token))

			_, user := fake.counts()
			assert.Zero(t, user, "a token rejected locally must not get a second opinion")
		})
	}
}

// The classic algorithm-confusion attack: sign with HS256 using the public
// key's bytes as the secret. It must never reach the JWKS path, and with no
// HMAC secret configured it goes to Supabase, which rejects it.
func TestParseToken_HS256SignedWithPublicKeyIsNotAccepted(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)

	key := jwksNewECKey(t)
	fake := jwksStartFake(t, jwksECPublic(t, "kid-1", &key.PublicKey))
	pub, err := key.PublicKey.Bytes()
	require.NoError(t, err)

	forged := jwksSign(t, jwt.SigningMethodHS256, "kid-1", pub, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Empty(t, parseToken(forged))
	jwks, user := fake.counts()
	assert.Zero(t, jwks, "an HS256 token must not consult the key set")
	assert.Equal(t, 1, user)
}

func TestParseToken_ES256_FallsBackWhenKeySetUnavailable(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1) // expected "jwks fetch failed" warn line

	key := jwksNewECKey(t)
	fake := jwksStartFake(t)
	fake.jwksStatus = http.StatusInternalServerError
	fake.userStatus, fake.userBody = http.StatusOK, `{"id":"`+jwksTestSub+`"}`
	token := jwksSign(t, jwt.SigningMethodES256, "kid-1", key, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Equal(t, jwksTestSub, parseToken(token))
	assert.Equal(t, jwksTestSub, parseToken(token))

	jwks, user := fake.counts()
	assert.Equal(t, 1, jwks, "a failing key set must not be refetched on every request")
	assert.Equal(t, 2, user, "Supabase decides while the key set is unavailable")
}

// Key rotation: a token arrives signed by a key published after our last
// fetch. The unknown kid must trigger a refetch rather than a rejection.
func TestParseToken_ES256_RefetchesOnUnknownKid(t *testing.T) {
	jwksSetRefetchInterval(t, 0)

	oldKey, newKey := jwksNewECKey(t), jwksNewECKey(t)
	fake := jwksStartFake(t, jwksECPublic(t, "old", &oldKey.PublicKey))

	oldToken := jwksSign(t, jwt.SigningMethodES256, "old", oldKey, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))
	require.Equal(t, jwksTestSub, parseToken(oldToken))

	fake.setKeys(jwksECPublic(t, "old", &oldKey.PublicKey), jwksECPublic(t, "new", &newKey.PublicKey))
	newToken := jwksSign(t, jwt.SigningMethodES256, "new", newKey, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Equal(t, jwksTestSub, parseToken(newToken))
	jwks, user := fake.counts()
	assert.Equal(t, 2, jwks)
	assert.Zero(t, user)
}

// Within the rate limit, an unknown kid is not refetched; Supabase decides.
func TestParseToken_ES256_UnknownKidWithinRateLimitFallsBack(t *testing.T) {
	known, unknown := jwksNewECKey(t), jwksNewECKey(t)
	fake := jwksStartFake(t, jwksECPublic(t, "known", &known.PublicKey))

	require.Equal(t, jwksTestSub, parseToken(
		jwksSign(t, jwt.SigningMethodES256, "known", known, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))))

	umwQuietLogs(t, log.LevelError+1) // expected non-OK line from the fallback
	stranger := jwksSign(t, jwt.SigningMethodES256, "never-published", unknown, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))
	assert.Empty(t, parseToken(stranger))

	jwks, user := fake.counts()
	assert.Equal(t, 1, jwks)
	assert.Equal(t, 1, user)
}

func TestParseToken_RS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	enc := base64.RawURLEncoding.EncodeToString
	fake := jwksStartFake(t, map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "rsa-1",
		"n": enc(key.N.Bytes()), "e": enc(big.NewInt(int64(key.E)).Bytes()),
	})
	token := jwksSign(t, jwt.SigningMethodRS256, "rsa-1", key, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Equal(t, jwksTestSub, parseToken(token))
	_, user := fake.counts()
	assert.Zero(t, user)
}

// An ES256 token whose kid names an RSA key must be rejected, not verified
// with the wrong kind of key.
func TestParseToken_AlgMustMatchKeyType(t *testing.T) {
	umwQuietLogs(t, log.LevelError+1)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ecKey := jwksNewECKey(t)
	enc := base64.RawURLEncoding.EncodeToString
	jwksStartFake(t, map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "rsa-1",
		"n": enc(rsaKey.N.Bytes()), "e": enc(big.NewInt(int64(rsaKey.E)).Bytes()),
	})

	token := jwksSign(t, jwt.SigningMethodES256, "rsa-1", ecKey, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))
	assert.Empty(t, parseToken(token))
}

func TestJWKPublicKey_RejectsUnusableKeys(t *testing.T) {
	enc := base64.RawURLEncoding.EncodeToString
	offCurve := make([]byte, 32)
	offCurve[31] = 1

	tests := []struct {
		name string
		key  jwk
	}{
		{"unsupported key type", jwk{Kty: "OKP", Crv: "Ed25519", X: enc(offCurve)}},
		{"unsupported curve", jwk{Kty: "EC", Crv: "P-384", X: enc(offCurve), Y: enc(offCurve)}},
		{"point not on curve", jwk{Kty: "EC", Crv: "P-256", X: enc(offCurve), Y: enc(offCurve)}},
		{"bad base64", jwk{Kty: "EC", Crv: "P-256", X: "!!!", Y: enc(offCurve)}},
		{"short rsa modulus", jwk{Kty: "RSA", N: enc(make([]byte, 128)), E: enc([]byte{1, 0, 1})}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.key.publicKey()
			assert.Error(t, err)
		})
	}
}

func TestTokenAlg(t *testing.T) {
	key := jwksNewECKey(t)
	es := jwksSign(t, jwt.SigningMethodES256, "k", key, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))
	hs := umwSignToken(t, umwTestSecret, umwUserClaims(jwksTestSub, time.Now().Add(time.Hour)))

	assert.Equal(t, "ES256", tokenAlg(es))
	assert.Equal(t, "HS256", tokenAlg(hs))
	assert.Empty(t, tokenAlg("tok-123"))
	assert.Empty(t, tokenAlg(""))
	assert.Empty(t, tokenAlg("not-base64!.x.y"))
}
