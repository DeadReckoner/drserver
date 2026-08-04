package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Provider configuration: which AI backend this process uses, and whether it is
// actually usable.
//
// NOTHING in this file logs, returns, or embeds a key value — not the key, not
// its length, not a prefix. Errors name the ENVIRONMENT VARIABLE that is wrong,
// which is all an operator needs and all that is safe to put in a log
// aggregator.

// minProviderKeyLen is a shape check, not a validation of the key itself: every
// current OpenAI and Anthropic key is far longer than this. It exists so an
// obviously-truncated value (a half-pasted secret, a placeholder like "TODO")
// is caught at startup rather than producing 401s from the provider on the
// first real generation.
const minProviderKeyLen = 20

var (
	// ErrUnknownProvider means AI_PROVIDER held a value this build does not
	// implement. Failing is deliberate: silently falling back to OpenAI meant a
	// typo in a deploy config sent traffic to a backend nobody chose.
	ErrUnknownProvider = errors.New("unknown AI_PROVIDER value")

	// ErrProviderKeyMissing means the selected provider's key env var is unset
	// or empty.
	ErrProviderKeyMissing = errors.New("AI provider API key is not set")

	// ErrProviderKeyTooShort means the key env var is set but implausibly short.
	ErrProviderKeyTooShort = errors.New("AI provider API key is too short to be valid")

	// ErrMockProviderHosted means AI_PROVIDER=mock reached a hosted runtime.
	ErrMockProviderHosted = errors.New("AI_PROVIDER=mock is refused on a hosted runtime")
)

// keyEnvForProvider names the environment variable each hosted provider reads.
func keyEnvForProvider(p ProviderType) string {
	switch p {
	case ProviderAnthropic:
		return "ANTHROPIC_API_KEY"
	case ProviderOpenAI:
		return "OPENAI_API_KEY"
	}
	return ""
}

// ResolveProviderType maps AI_PROVIDER onto a ProviderType.
//
// An empty value keeps the historical default (OpenAI) so existing deploys and
// every test that never sets the variable behave exactly as before. Any other
// unrecognised value is an error.
func ResolveProviderType(raw string) (ProviderType, error) {
	switch p := ProviderType(strings.TrimSpace(strings.ToLower(raw))); p {
	case "":
		return ProviderOpenAI, nil
	case ProviderOpenAI, ProviderAnthropic, ProviderMock:
		return p, nil
	default:
		// The provider NAME is not a secret, so echoing it is safe and is the
		// only way an operator can find the typo.
		return "", fmt.Errorf("%w: %q (expected one of: openai, anthropic, mock)", ErrUnknownProvider, raw)
	}
}

// ValidateProviderConfig checks the whole AI backend configuration and returns
// the first problem it finds. Call it once at startup: a process that cannot
// generate anything should say so immediately, not on the first user request.
//
// Checks, in order:
//   - AI_PROVIDER names a backend this build implements;
//   - mock is not selected on a hosted runtime;
//   - the selected hosted provider's key env var is set and plausibly shaped.
func ValidateProviderConfig() error {
	provider, err := ResolveProviderType(os.Getenv("AI_PROVIDER"))
	if err != nil {
		return err
	}

	if provider == ProviderMock {
		if isHostedRuntime() {
			return fmt.Errorf("%w: the mock backend returns fabricated plans and must never serve real users", ErrMockProviderHosted)
		}
		// Local mock needs no key: it never makes a network call.
		return nil
	}

	_, err = requireKey(keyEnvForProvider(provider))
	return err
}

// requireKey reads a key env var and enforces presence and a minimum length.
// The value is returned to the caller but never logged or wrapped into an
// error.
func requireKey(envName string) (string, error) {
	if envName == "" {
		return "", fmt.Errorf("%w: no key variable is defined for the selected provider", ErrProviderKeyMissing)
	}
	key := strings.TrimSpace(os.Getenv(envName))
	if key == "" {
		return "", fmt.Errorf("%w: set %s", ErrProviderKeyMissing, envName)
	}
	if len(key) < minProviderKeyLen {
		return "", fmt.Errorf("%w: %s is set but shorter than %d characters", ErrProviderKeyTooShort, envName, minProviderKeyLen)
	}
	return key, nil
}

// RequireOpenAIKey returns the OpenAI key or a typed error. Used on the request
// path so a missing key produces a failed generation rather than a panic in a
// background goroutine that gin's Recovery middleware cannot see.
func RequireOpenAIKey() (string, error) {
	return requireKey("OPENAI_API_KEY")
}
