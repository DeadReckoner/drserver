package types

import (
	"os"
	"strings"
)

// defaultProWhitelistDomain is the built-in email domain that is always
// granted Pro. Overridable/extendable via the PRO_WHITELIST_DOMAINS env var
// (comma-separated). The default is kept here so the feature works with zero
// configuration.
const defaultProWhitelistDomain = "deadreckoner.io"

// ProWhitelistDomains returns the set of email domains whose users are
// granted Pro access without a paid subscription. Driven by the
// PRO_WHITELIST_DOMAINS env var (comma-separated, e.g.
// "deadreckoner.io,example.com"); falls back to the built-in default when the
// var is empty or unset.
//
// Domains are lowercased and trimmed. Empty entries are dropped.
func ProWhitelistDomains() []string {
	raw := strings.TrimSpace(os.Getenv("PRO_WHITELIST_DOMAINS"))
	if raw == "" {
		return []string{defaultProWhitelistDomain}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		d := strings.ToLower(strings.TrimSpace(p))
		if d != "" {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return []string{defaultProWhitelistDomain}
	}
	return out
}

// IsProWhitelistedEmail reports whether the given email address belongs to a
// whitelisted domain and should therefore be granted Pro for free.
//
// Matching is on the exact domain part after the final "@", case-insensitive.
// We deliberately do NOT use a suffix/contains check: "evil-deadreckoner.io"
// and "deadreckoner.io.attacker.com" must NOT match "deadreckoner.io".
func IsProWhitelistedEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	for _, d := range ProWhitelistDomains() {
		if domain == d {
			return true
		}
	}
	return false
}
