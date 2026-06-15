package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsProWhitelistedEmail_DefaultDomain(t *testing.T) {
	t.Setenv("PRO_WHITELIST_DOMAINS", "")
	tests := []struct {
		email string
		want  bool
	}{
		{"mike@deadreckoner.io", true},
		{"MIKE@DEADRECKONER.IO", true},            // case-insensitive
		{"  sam@deadreckoner.io  ", true},         // trimmed
		{"a.b+tag@deadreckoner.io", true},         // plus addressing
		{"someone@gmail.com", false},              // other domain
		{"nobody@evil-deadreckoner.io", false},    // prefix spoof
		{"x@deadreckoner.io.attacker.com", false}, // suffix spoof
		{"deadreckoner.io", false},                // no @
		{"trailing@", false},                      // empty domain
		{"", false},                               // empty
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			assert.Equal(t, tt.want, IsProWhitelistedEmail(tt.email))
		})
	}
}

func TestIsProWhitelistedEmail_ConfiguredDomains(t *testing.T) {
	t.Setenv("PRO_WHITELIST_DOMAINS", "example.com, Foo.Org ")
	assert.True(t, IsProWhitelistedEmail("a@example.com"))
	assert.True(t, IsProWhitelistedEmail("b@foo.org"), "configured domains are lowercased")
	// Default is REPLACED, not appended, when the env var is set.
	assert.False(t, IsProWhitelistedEmail("c@deadreckoner.io"))
}

func TestProWhitelistDomains_FallsBackOnEmptyEntries(t *testing.T) {
	t.Setenv("PRO_WHITELIST_DOMAINS", " , , ")
	assert.Equal(t, []string{defaultProWhitelistDomain}, ProWhitelistDomains())
}

func TestProWhitelistDomains_Default(t *testing.T) {
	t.Setenv("PRO_WHITELIST_DOMAINS", "")
	assert.Equal(t, []string{"deadreckoner.io"}, ProWhitelistDomains())
}
