package agent

import (
	"testing"

	"dr/server/agent/anthropic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time checks that both concrete providers satisfy the Provider
// interface. (anthropic.Provider is verified via GetProvider below since the
// interface lives here.)
var _ Provider = (*OpenAIProvider)(nil)

func TestGetProviderSelection(t *testing.T) {
	t.Run("anthropic type returns anthropic provider", func(t *testing.T) {
		p := GetProvider(ProviderAnthropic)
		require.NotNil(t, p)
		assert.Equal(t, "anthropic", p.Name())
		assert.IsType(t, &anthropic.Provider{}, p)
	})

	t.Run("openai type returns openai provider", func(t *testing.T) {
		p := GetProvider(ProviderOpenAI)
		require.NotNil(t, p)
		assert.Equal(t, "openai", p.Name())
		assert.IsType(t, &OpenAIProvider{}, p)
	})

	t.Run("unknown type falls through to openai default", func(t *testing.T) {
		p := GetProvider(ProviderType("bogus"))
		require.NotNil(t, p)
		assert.Equal(t, "openai", p.Name())
		assert.IsType(t, &OpenAIProvider{}, p)
	})
}

func TestNewOpenAIProvider(t *testing.T) {
	p := NewOpenAIProvider()
	require.NotNil(t, p)
	assert.Equal(t, string(ProviderOpenAI), p.Name())
}
