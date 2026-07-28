package apple

// In-package tests for Sign in with Apple token revocation. The Client is
// constructed directly with a freshly generated ECDSA P-256 key and an
// injected http.RoundTripper that intercepts the hardcoded appleid.apple.com
// URLs, so no test ever touches the network. The client-secret JWT is
// verified against the generated public key with the same golang-jwt library
// production uses.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	infraTeamID   = "TEAM123456"
	infraKeyID    = "KEYID12345"
	infraClientID = "com.example.deadreckoner"
)

// infraGenPKCS8Key generates an ECDSA P-256 key and returns it alongside its
// PKCS#8 PEM encoding — the same shape as a real .p8 file from Apple.
func infraGenPKCS8Key(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	require.NotNil(t, pemBytes)
	return key, pemBytes
}

// infraAppleCall records one intercepted request to an Apple endpoint.
type infraAppleCall struct {
	url         string
	contentType string
	form        url.Values
}

// infraAppleTransport is a RoundTripper that records every request (URL,
// Content-Type, decoded form body) and answers with whatever handler returns
// for the request URL. All usage is single-goroutine, so no locking needed.
type infraAppleTransport struct {
	calls   []infraAppleCall
	handler func(reqURL string) (status int, body string)
}

func (tr *infraAppleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, err
	}
	tr.calls = append(tr.calls, infraAppleCall{
		url:         req.URL.String(),
		contentType: req.Header.Get("Content-Type"),
		form:        form,
	})
	status, respBody := tr.handler(req.URL.String())
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Request:    req,
	}, nil
}

// infraNewTestClient builds a fully configured Client whose HTTP layer is the
// given transport. Returns the signing key so tests can verify JWTs.
func infraNewTestClient(t *testing.T, rt http.RoundTripper) (*Client, *ecdsa.PrivateKey) {
	t.Helper()
	key, pemBytes := infraGenPKCS8Key(t)
	return &Client{
		teamID:     infraTeamID,
		keyID:      infraKeyID,
		clientID:   infraClientID,
		privateKey: pemBytes,
		httpClient: &http.Client{Transport: rt},
	}, key
}

func TestInfraAppleClientSecret_SignsVerifiableES256JWT(t *testing.T) {
	c, key := infraNewTestClient(t, nil) // no HTTP involved

	secret, err := c.clientSecret()
	require.NoError(t, err)
	require.NotEmpty(t, secret)

	parsed, err := jwt.Parse(secret, func(tok *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	require.NoError(t, err)
	require.True(t, parsed.Valid)

	assert.Equal(t, "ES256", parsed.Method.Alg())
	assert.Equal(t, infraKeyID, parsed.Header["kid"])

	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	assert.Equal(t, infraTeamID, claims["iss"])
	assert.Equal(t, appleAudience, claims["aud"])
	assert.Equal(t, infraClientID, claims["sub"])

	// exp is exactly iat + 5 minutes (relative check, no wall-clock coupling).
	iat, ok := claims["iat"].(float64)
	require.True(t, ok)
	exp, ok := claims["exp"].(float64)
	require.True(t, ok)
	assert.Equal(t, float64(5*60), exp-iat)
}

func TestInfraAppleClientSecret_KeyErrors(t *testing.T) {
	// SEC1 EC encoding is what `openssl ecparam` emits; Apple's .p8 files are
	// PKCS#8, and clientSecret only accepts PKCS#8.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	sec1DER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	sec1PEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1DER})

	cases := []struct {
		name       string
		privateKey []byte
		wantErr    string
	}{
		{
			name:       "not valid PEM",
			privateKey: []byte("this is not a pem block"),
			wantErr:    "not valid PEM",
		},
		{
			name:       "valid PEM but SEC1 EC key instead of PKCS#8",
			privateKey: sec1PEM,
			wantErr:    "parse private key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{
				teamID:     infraTeamID,
				keyID:      infraKeyID,
				clientID:   infraClientID,
				privateKey: tc.privateKey,
			}
			secret, err := c.clientSecret()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Empty(t, secret)
		})
	}
}

func TestInfraAppleRevokeByAuthCode_GuardClauses(t *testing.T) {
	t.Run("nil client is a configuration error", func(t *testing.T) {
		var c *Client
		err := c.RevokeByAuthCode(context.Background(), "some-code")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "client not configured")
	})

	t.Run("empty auth code rejected before any HTTP call", func(t *testing.T) {
		tr := &infraAppleTransport{handler: func(string) (int, string) { return 200, "" }}
		c, _ := infraNewTestClient(t, tr)
		err := c.RevokeByAuthCode(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "auth code is required")
		assert.Empty(t, tr.calls, "no HTTP request should have been made")
	})
}

func TestInfraAppleRevokeByAuthCode_HappyPath(t *testing.T) {
	tr := &infraAppleTransport{handler: func(reqURL string) (int, string) {
		switch reqURL {
		case appleTokenURL:
			return 200, `{"refresh_token":"rt-abc"}`
		case appleRevokeURL:
			return 200, ""
		default:
			return 500, "unexpected URL"
		}
	}}
	c, key := infraNewTestClient(t, tr)

	err := c.RevokeByAuthCode(context.Background(), "auth-code-123")
	require.NoError(t, err)

	// Exactly two calls, in exchange-then-revoke order.
	require.Len(t, tr.calls, 2)
	exchange, revoke := tr.calls[0], tr.calls[1]
	assert.Equal(t, appleTokenURL, exchange.url)
	assert.Equal(t, appleRevokeURL, revoke.url)

	// Both requests are form-encoded.
	assert.Equal(t, "application/x-www-form-urlencoded", exchange.contentType)
	assert.Equal(t, "application/x-www-form-urlencoded", revoke.contentType)

	// Exchange form carries the auth code and OAuth grant type.
	assert.Equal(t, infraClientID, exchange.form.Get("client_id"))
	assert.Equal(t, "auth-code-123", exchange.form.Get("code"))
	assert.Equal(t, "authorization_code", exchange.form.Get("grant_type"))
	require.NotEmpty(t, exchange.form.Get("client_secret"))

	// The client_secret in the form is a verifiable JWT for our key.
	_, err = jwt.Parse(exchange.form.Get("client_secret"), func(tok *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	assert.NoError(t, err)

	// Revoke form targets the refresh token from the exchange step.
	assert.Equal(t, infraClientID, revoke.form.Get("client_id"))
	assert.Equal(t, "rt-abc", revoke.form.Get("token"))
	assert.Equal(t, "refresh_token", revoke.form.Get("token_type_hint"))
	assert.NotEmpty(t, revoke.form.Get("client_secret"))
}

func TestInfraAppleRevokeByAuthCode_ExchangeErrors(t *testing.T) {
	cases := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		wantErr     []string
	}{
		{
			name:        "200 with apple error payload",
			tokenStatus: 200,
			tokenBody:   `{"error":"invalid_grant","error_description":"The code has expired"}`,
			wantErr:     []string{"apple: exchange code", "invalid_grant", "The code has expired"},
		},
		{
			name:        "200 missing refresh_token",
			tokenStatus: 200,
			tokenBody:   `{}`,
			wantErr:     []string{"apple: exchange code", "did not include refresh_token"},
		},
		{
			name:        "non-2xx status surfaces status and body",
			tokenStatus: 400,
			tokenBody:   "bad request body",
			wantErr:     []string{"apple: exchange code", "apple responded 400: bad request body"},
		},
		{
			name:        "malformed JSON body",
			tokenStatus: 200,
			tokenBody:   `not-json{`,
			wantErr:     []string{"apple: exchange code", "decode response"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &infraAppleTransport{handler: func(reqURL string) (int, string) {
				if reqURL == appleTokenURL {
					return tc.tokenStatus, tc.tokenBody
				}
				return 500, "revoke should never be reached"
			}}
			c, _ := infraNewTestClient(t, tr)

			err := c.RevokeByAuthCode(context.Background(), "auth-code-123")
			require.Error(t, err)
			for _, want := range tc.wantErr {
				assert.Contains(t, err.Error(), want)
			}
			// A failed exchange must short-circuit before the revoke call.
			assert.Len(t, tr.calls, 1)
		})
	}
}

func TestInfraAppleRevokeByAuthCode_RevokeFailure(t *testing.T) {
	tr := &infraAppleTransport{handler: func(reqURL string) (int, string) {
		if reqURL == appleTokenURL {
			return 200, `{"refresh_token":"rt-abc"}`
		}
		return 500, "oops"
	}}
	c, _ := infraNewTestClient(t, tr)

	err := c.RevokeByAuthCode(context.Background(), "auth-code-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apple: revoke token")
	assert.Contains(t, err.Error(), "apple responded 500: oops")
	assert.Len(t, tr.calls, 2)
}

func TestInfraApplePostForm_EmptyBodyWithOut(t *testing.T) {
	// A 2xx with an empty body must not attempt JSON decoding even when the
	// caller passed a non-nil out (the len(body)==0 branch).
	tr := &infraAppleTransport{handler: func(string) (int, string) { return 200, "" }}
	c, _ := infraNewTestClient(t, tr)

	var out tokenResponse
	err := c.postForm(context.Background(), appleTokenURL, url.Values{"a": {"b"}}, &out)
	require.NoError(t, err)
	assert.Equal(t, tokenResponse{}, out, "out must be left untouched")
}

func TestInfraAppleNewClientFromEnv(t *testing.T) {
	// t.Setenv scopes each variable to the subtest and restores prior values,
	// so these cases stay hermetic even on machines with APPLE_* configured.
	setAll := func(t *testing.T, teamID, keyID, clientID, privateKey string) {
		t.Setenv("APPLE_TEAM_ID", teamID)
		t.Setenv("APPLE_KEY_ID", keyID)
		t.Setenv("APPLE_CLIENT_ID", clientID)
		t.Setenv("APPLE_PRIVATE_KEY", privateKey)
	}

	t.Run("all vars empty is a graceful no-op", func(t *testing.T) {
		setAll(t, "", "", "", "")
		c, err := NewClientFromEnv()
		require.NoError(t, err)
		assert.Nil(t, c)
	})

	t.Run("partial configuration is an error", func(t *testing.T) {
		setAll(t, infraTeamID, "", "", "")
		c, err := NewClientFromEnv()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must all be set")
		assert.Nil(t, c)
	})

	t.Run("fully configured converts escaped newlines", func(t *testing.T) {
		setAll(t, infraTeamID, infraKeyID, infraClientID,
			"-----BEGIN PRIVATE KEY-----\\nabc123\\n-----END PRIVATE KEY-----")
		c, err := NewClientFromEnv()
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Equal(t, infraTeamID, c.teamID)
		assert.Equal(t, infraKeyID, c.keyID)
		assert.Equal(t, infraClientID, c.clientID)
		assert.Equal(t,
			"-----BEGIN PRIVATE KEY-----\nabc123\n-----END PRIVATE KEY-----",
			string(c.privateKey),
			"literal \\n sequences must become real newlines")
		require.NotNil(t, c.httpClient)
		assert.Equal(t, 10*time.Second, c.httpClient.Timeout)
	})
}
