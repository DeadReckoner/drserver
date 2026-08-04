package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Provider configuration is the difference between a process that can generate
// and one that fails every request at runtime, so it is validated at startup.
// These tests also pin the security property: NOTHING here may put a key value,
// or any fragment of one, into an error string.

// aValidLookingKey is long enough to pass the shape check. It is not a real
// credential and never leaves this file.
const aValidLookingKey = "sk-test-000000000000000000000000"

func clearProviderEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AI_PROVIDER", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("FLY_APP_NAME", "")
}

func TestResolveProviderType(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    ProviderType
		wantErr error
	}{
		// Empty keeps the historical default so existing deploys, and every
		// test that never sets the variable, behave exactly as before.
		{name: "empty defaults to openai", raw: "", want: ProviderOpenAI},
		{name: "whitespace defaults to openai", raw: "   ", want: ProviderOpenAI},
		{name: "openai", raw: "openai", want: ProviderOpenAI},
		{name: "anthropic", raw: "anthropic", want: ProviderAnthropic},
		{name: "mock", raw: "mock", want: ProviderMock},
		{name: "case insensitive", raw: "Anthropic", want: ProviderAnthropic},
		{name: "unknown is an error", raw: "gpt5-turbo-max", wantErr: ErrUnknownProvider},
		{name: "near miss is an error", raw: "open-ai", wantErr: ErrUnknownProvider},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveProviderType(tc.raw)
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateProviderConfig_OpenAIKeyMissing(t *testing.T) {
	clearProviderEnv(t)

	err := ValidateProviderConfig()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderKeyMissing)
	// The remedy must be actionable: name the variable.
	assert.Contains(t, err.Error(), "OPENAI_API_KEY")
}

func TestValidateProviderConfig_OpenAIKeyEmptyAfterTrim(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "   \t ")

	err := ValidateProviderConfig()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderKeyMissing)
}

// A half-pasted secret used to reach the request path and panic there. It is a
// configuration error and must be caught at startup.
func TestValidateProviderConfig_OpenAIKeyTooShort(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-abc")

	err := ValidateProviderConfig()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderKeyTooShort)
	// The value must NOT appear in the message, not even a truncated key.
	assert.NotContains(t, err.Error(), "sk-abc")
}

func TestValidateProviderConfig_OpenAIKeyPresent(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", aValidLookingKey)

	assert.NoError(t, ValidateProviderConfig())
}

func TestValidateProviderConfig_AnthropicRequiresItsOwnKey(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("AI_PROVIDER", "anthropic")
	// An OpenAI key is present but irrelevant: the SELECTED provider's key is
	// what has to exist.
	t.Setenv("OPENAI_API_KEY", aValidLookingKey)

	err := ValidateProviderConfig()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderKeyMissing)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")

	t.Setenv("ANTHROPIC_API_KEY", aValidLookingKey)
	assert.NoError(t, ValidateProviderConfig())
}

func TestValidateProviderConfig_UnknownProviderFailsStartup(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("AI_PROVIDER", "definitely-not-a-provider")
	t.Setenv("OPENAI_API_KEY", aValidLookingKey)

	err := ValidateProviderConfig()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownProvider)
	// Naming the accepted values is the whole point of failing loudly.
	assert.Contains(t, err.Error(), "openai")
	assert.Contains(t, err.Error(), "anthropic")
}

// Mock is the deterministic offline backend. It needs no key because it never
// makes a network call.
func TestValidateProviderConfig_MockLocalNeedsNoKey(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("AI_PROVIDER", "mock")

	assert.NoError(t, ValidateProviderConfig())
}

// The one configuration that must never start: fabricated plans served to real
// users. FLY_APP_NAME is injected on every Fly machine.
func TestValidateProviderConfig_MockRefusedOnHostedRuntime(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("AI_PROVIDER", "mock")
	t.Setenv("FLY_APP_NAME", "drserver-purple-moon-7907")

	err := ValidateProviderConfig()

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMockProviderHosted)
}

func TestRequireOpenAIKey_ReturnsErrorNotPanic(t *testing.T) {
	clearProviderEnv(t)

	// The old code sliced the key for a log line, which panicked on an unset or
	// short value — inside a background goroutine, where gin's Recovery
	// middleware cannot see it, taking the whole process down.
	assert.NotPanics(t, func() {
		_, err := RequireOpenAIKey()
		assert.ErrorIs(t, err, ErrProviderKeyMissing)
	})

	t.Setenv("OPENAI_API_KEY", "short")
	assert.NotPanics(t, func() {
		_, err := RequireOpenAIKey()
		assert.ErrorIs(t, err, ErrProviderKeyTooShort)
	})

	t.Setenv("OPENAI_API_KEY", aValidLookingKey)
	key, err := RequireOpenAIKey()
	require.NoError(t, err)
	assert.Equal(t, aValidLookingKey, key)
}

// Regression guard for the log line that printed apiKey[:10] and the last five
// characters of the key on every OpenAI request.
func TestNoKeyMaterialInErrors(t *testing.T) {
	clearProviderEnv(t)
	const secret = "sk-verysecretvalue-abcdefghijklmnop"
	t.Setenv("OPENAI_API_KEY", secret)
	t.Setenv("AI_PROVIDER", "anthropic")

	err := ValidateProviderConfig()
	require.Error(t, err)

	for _, fragment := range []string{secret, secret[:10], secret[len(secret)-5:]} {
		assert.False(t, strings.Contains(err.Error(), fragment),
			"an error message must never carry key material, not even a prefix or suffix")
	}
}

func TestDefaultProvider_PanicsOnUnknownProvider(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("AI_PROVIDER", "nope")

	// Silently falling back to OpenAI turned a typo in a deploy config into
	// traffic on a backend nobody chose.
	assert.PanicsWithValue(t, resolveErrText(t, "nope"), func() { DefaultProvider() })
}

func resolveErrText(t *testing.T, raw string) string {
	t.Helper()
	_, err := ResolveProviderType(raw)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnknownProvider))
	return err.Error()
}

func TestDefaultProvider_SelectsWithoutRequiringAKey(t *testing.T) {
	clearProviderEnv(t)

	// Selection and key validation are deliberately separate: key presence is a
	// startup concern (ValidateProviderConfig), which is what lets the handler
	// tests build a router without a real credential.
	assert.NotPanics(t, func() {
		assert.Equal(t, string(ProviderOpenAI), DefaultProvider().Name())
	})
}
