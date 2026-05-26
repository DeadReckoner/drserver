// Package apple implements Sign in with Apple token revocation, used when a
// user deletes their account. Apple requires deleted accounts to have their
// refresh tokens revoked via /auth/token + /auth/revoke; otherwise the app
// risks rejection from App Store Review.
package apple

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	appleTokenURL  = "https://appleid.apple.com/auth/token"
	appleRevokeURL = "https://appleid.apple.com/auth/revoke"
	appleAudience  = "https://appleid.apple.com"
)

// Client revokes Sign in with Apple refresh tokens.
type Client struct {
	teamID     string
	keyID      string
	clientID   string
	privateKey []byte
	httpClient *http.Client
}

// NewClientFromEnv builds a Client from APPLE_TEAM_ID, APPLE_KEY_ID,
// APPLE_CLIENT_ID, APPLE_PRIVATE_KEY. APPLE_PRIVATE_KEY is the PEM-encoded
// .p8 contents (literal newlines or "\n" escapes both accepted). Returns nil
// without error when env vars are missing, so callers can no-op gracefully in
// environments where Apple isn't configured (local dev, tests).
func NewClientFromEnv() (*Client, error) {
	teamID := os.Getenv("APPLE_TEAM_ID")
	keyID := os.Getenv("APPLE_KEY_ID")
	clientID := os.Getenv("APPLE_CLIENT_ID")
	privateKey := os.Getenv("APPLE_PRIVATE_KEY")

	if teamID == "" && keyID == "" && clientID == "" && privateKey == "" {
		return nil, nil
	}
	if teamID == "" || keyID == "" || clientID == "" || privateKey == "" {
		return nil, errors.New("apple: APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_CLIENT_ID, APPLE_PRIVATE_KEY must all be set")
	}

	return &Client{
		teamID:     teamID,
		keyID:      keyID,
		clientID:   clientID,
		privateKey: []byte(strings.ReplaceAll(privateKey, "\\n", "\n")),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// RevokeByAuthCode exchanges the authorization code from the mobile client for
// a refresh token, then revokes that refresh token. Both steps must succeed
// for Apple to consider the user's grant fully revoked.
func (c *Client) RevokeByAuthCode(ctx context.Context, authCode string) error {
	if c == nil {
		return errors.New("apple: client not configured")
	}
	if authCode == "" {
		return errors.New("apple: auth code is required")
	}

	clientSecret, err := c.clientSecret()
	if err != nil {
		return err
	}

	refreshToken, err := c.exchangeCode(ctx, authCode, clientSecret)
	if err != nil {
		return fmt.Errorf("apple: exchange code: %w", err)
	}

	if err := c.revokeToken(ctx, refreshToken, clientSecret); err != nil {
		return fmt.Errorf("apple: revoke token: %w", err)
	}
	return nil
}

// clientSecret builds the short-lived ES256 JWT Apple expects in place of a
// traditional OAuth client secret.
func (c *Client) clientSecret() (string, error) {
	block, _ := pem.Decode(c.privateKey)
	if block == nil {
		return "", errors.New("apple: private key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("apple: parse private key: %w", err)
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": c.teamID,
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
		"aud": appleAudience,
		"sub": c.clientID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = c.keyID
	return token.SignedString(key)
}

func (c *Client) exchangeCode(ctx context.Context, authCode, clientSecret string) (string, error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {clientSecret},
		"code":          {authCode},
		"grant_type":    {"authorization_code"},
	}

	var resp tokenResponse
	if err := c.postForm(ctx, appleTokenURL, form, &resp); err != nil {
		return "", err
	}
	if resp.RefreshToken == "" {
		return "", errors.New("response did not include refresh_token")
	}
	return resp.RefreshToken, nil
}

func (c *Client) revokeToken(ctx context.Context, refreshToken, clientSecret string) error {
	form := url.Values{
		"client_id":       {c.clientID},
		"client_secret":   {clientSecret},
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
	}
	return c.postForm(ctx, appleRevokeURL, form, nil)
}

type tokenResponse struct {
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, out *tokenResponse) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("apple responded %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if out.Error != "" {
			return fmt.Errorf("apple error %q: %s", out.Error, out.ErrorDesc)
		}
	}
	return nil
}
