package middleware

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Asymmetric (JWKS) verification of Supabase access tokens.
//
// A project that has moved to Supabase's asymmetric JWT signing keys signs
// access tokens with a private key it never shares (ES256, or RS256), and
// publishes the matching public keys at
// <API_URL>/auth/v1/.well-known/jwks.json. There is no shared secret to put in
// the environment, so the HS256 path in verifyTokenLocally cannot verify these
// tokens at all. This file verifies them in-process against the published
// keys, which keeps authentication off the network the same way the HS256
// path does.

const (
	jwksPath = "/auth/v1/.well-known/jwks.json"

	// jwksMaxBytes bounds the response we are willing to parse. A real key
	// set is well under a kilobyte per key.
	jwksMaxBytes = 1 << 20
)

var (
	// jwksTTL is how long a fetched key set is trusted before it is fetched
	// again. Keys rotate rarely, and a token signed by a key we have not seen
	// triggers its own refetch (see jwksMinRefetch), so this only bounds how
	// long a *revoked* key keeps verifying.
	jwksTTL = time.Hour

	// jwksMinRefetch rate-limits fetches. Without it, a stream of tokens
	// carrying an unknown kid — or a JWKS endpoint that is down — would turn
	// every authenticated request into an outbound fetch.
	jwksMinRefetch = time.Minute

	// asymmetricAlgs are the signing methods accepted on the JWKS path. HS256
	// is deliberately absent: accepting it here would let a caller sign a
	// token using a public key's bytes as the HMAC secret.
	asymmetricAlgs = []string{jwt.SigningMethodES256.Alg(), jwt.SigningMethodRS256.Alg()}

	// errJWKSUnavailable means the key needed to verify a token could not be
	// obtained — the fetch failed, or the key set does not contain the
	// token's kid. It says nothing about whether the token is valid.
	errJWKSUnavailable = errors.New("jwks: signing key unavailable")

	supabaseJWKS = &jwksCache{}
)

// jwksCache holds the public keys published by one Supabase project.
type jwksCache struct {
	mu          sync.Mutex
	url         string
	keys        map[string]crypto.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

// key returns the public key for kid, fetching the key set when it is
// missing, stale, or does not contain kid.
//
// The fetch happens with the lock held so concurrent requests share one
// fetch instead of each starting their own. authHTTPClient's timeout bounds
// how long that can take, and jwksMinRefetch stops a failing endpoint from
// being retried by every request behind it.
func (c *jwksCache) key(url, kid string) (crypto.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.url != url {
		// API_URL changed (in practice: tests). Keys for another project
		// must never verify this one's tokens.
		c.url, c.keys, c.fetchedAt, c.lastAttempt = url, nil, time.Time{}, time.Time{}
	}

	key, found := c.keys[kid]
	stale := c.keys == nil || now.Sub(c.fetchedAt) > jwksTTL
	if (stale || !found) && now.Sub(c.lastAttempt) >= jwksMinRefetch {
		c.lastAttempt = now
		keys, err := fetchJWKS(url)
		if err != nil {
			authLog.Warn("supabase auth: jwks fetch failed", "detail", err.Error())
		} else {
			c.keys, c.fetchedAt = keys, now
			key, found = keys[kid]
		}
	}

	if !found {
		return nil, errJWKSUnavailable
	}
	return key, nil
}

// jwk is the subset of a JSON Web Key we read.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchJWKS downloads and parses a key set, returning its usable signing
// keys by kid. Keys of a type we do not support are skipped rather than
// failing the whole set, so adding a new key type to a project cannot break
// verification with the keys we do understand.
func fetchJWKS(url string) (map[string]crypto.PublicKey, error) {
	resp, err := authHTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBytes))
	if err != nil {
		return nil, err
	}

	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("decode key set: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kid == "" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := k.publicKey()
		if err != nil {
			authLog.Warn("supabase auth: skipping unusable jwk", "kid", k.Kid, "detail", err.Error())
			continue
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

// publicKey converts a JWK into the key type golang-jwt verifies with.
func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "EC":
		if k.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported curve %q", k.Crv)
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, fmt.Errorf("decode x: %w", err)
		}
		y, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, fmt.Errorf("decode y: %w", err)
		}
		const size = 32 // P-256 coordinate length
		if len(x) > size || len(y) > size {
			return nil, errors.New("coordinate too long")
		}
		// Uncompressed SEC 1 point: 0x04 || X || Y, each left-padded. Parsing
		// it this way also rejects points that are not on the curve.
		point := make([]byte, 1+2*size)
		point[0] = 4
		copy(point[1+size-len(x):], x)
		copy(point[1+2*size-len(y):], y)
		return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)

	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("decode n: %w", err)
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("decode e: %w", err)
		}
		if len(n) < 256 { // 2048-bit minimum
			return nil, errors.New("rsa modulus too short")
		}
		exp := new(big.Int).SetBytes(e)
		if !exp.IsInt64() || exp.Int64() < 3 || exp.Int64() > 1<<31-1 {
			return nil, errors.New("rsa exponent out of range")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exp.Int64())}, nil
	}
	return nil, fmt.Errorf("unsupported key type %q", k.Kty)
}

// tokenAlg returns the "alg" from a token's header without verifying
// anything. It is used only to pick a verification path; the path it picks
// then enforces the algorithm itself.
func tokenAlg(accessToken string) string {
	header, _, ok := strings.Cut(accessToken, ".")
	if !ok {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		return ""
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(raw, &h) != nil {
		return ""
	}
	return h.Alg
}

// isAsymmetricAlg reports whether alg is one the JWKS path verifies.
func isAsymmetricAlg(alg string) bool {
	for _, a := range asymmetricAlgs {
		if a == alg {
			return true
		}
	}
	return false
}

// verifyTokenWithJWKS verifies an asymmetrically signed token against the
// project's published keys and returns its subject.
//
// decided is false when no verdict could be reached because the signing key
// was unavailable; the caller should then ask Supabase instead. When decided
// is true the result is final: a non-empty subject for a valid user token,
// "" for one that was verified and rejected.
func verifyTokenWithJWKS(accessToken string) (subject string, decided bool) {
	base := strings.TrimRight(os.Getenv("API_URL"), "/")
	if base == "" {
		return "", false
	}

	var claims supabaseClaims
	_, err := jwt.ParseWithClaims(accessToken, &claims,
		func(t *jwt.Token) (interface{}, error) {
			kid, _ := t.Header["kid"].(string)
			if kid == "" {
				return nil, errJWKSUnavailable
			}
			// The key's Go type pins the algorithm: golang-jwt refuses to
			// verify ES256 with an RSA key and vice versa.
			return supabaseJWKS.key(base+jwksPath, kid)
		},
		jwt.WithValidMethods(asymmetricAlgs),
	)
	if errors.Is(err, errJWKSUnavailable) {
		return "", false
	}
	return subjectFromVerifiedClaims(claims, err), true
}
