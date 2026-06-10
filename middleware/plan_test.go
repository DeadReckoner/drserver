package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGatingEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"unset", "", false},
		{"explicit false", "false", false},
		{"explicit 0", "0", false},
		{"random string", "maybe", false},
		{"true lowercase", "true", true},
		{"True mixed case", "True", true},
		{"TRUE uppercase", "TRUE", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"on", "on", true},
		{"  true with spaces  ", "  true  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PRO_GATING_ENABLED", tt.env)
			assert.Equal(t, tt.want, IsGatingEnabled())
		})
	}
}
