package revenuecat

import (
	"crypto/subtle"
	"errors"
	"os"
)

// Errors returned by VerifyAuth.
var (
	// ErrSecretNotConfigured indicates the REVENUECAT_WEBHOOK_SECRET env var
	// is unset. The handler maps this to 503 so a misconfigured prod doesn't
	// silently accept any signature.
	ErrSecretNotConfigured = errors.New("REVENUECAT_WEBHOOK_SECRET not configured")

	// ErrAuthMismatch indicates the Authorization header did not match the
	// configured shared secret. The handler maps this to 401.
	ErrAuthMismatch = errors.New("revenuecat: authorization header mismatch")
)

// VerifyAuth checks the provided Authorization header value against the
// REVENUECAT_WEBHOOK_SECRET env var using a constant-time compare.
//
// RevenueCat's webhook delivery sends the value configured in the dashboard's
// "Authorization header value" field verbatim in the Authorization header
// (no Bearer prefix). We require an exact match.
//
// Returns ErrSecretNotConfigured if the env var is missing, and
// ErrAuthMismatch on mismatch.
func VerifyAuth(authHeader string) error {
	secret := os.Getenv("REVENUECAT_WEBHOOK_SECRET")
	if secret == "" {
		return ErrSecretNotConfigured
	}
	if subtle.ConstantTimeCompare([]byte(authHeader), []byte(secret)) != 1 {
		return ErrAuthMismatch
	}
	return nil
}
