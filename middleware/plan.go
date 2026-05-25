package middleware

import (
	"os"
	"strings"
)

// IsGatingEnabled reports whether plan-limit enforcement should be applied to
// requests. Controlled by the PRO_GATING_ENABLED env var so we can ship the
// code dark and flip it on per environment.
//
// Truthy values: "1", "true", "yes", "on" (case-insensitive). Anything else,
// including unset, evaluates to false.
func IsGatingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PRO_GATING_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
