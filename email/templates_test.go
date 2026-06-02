package email

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// All templates must parse at package init; the global renderers map would
// panic otherwise. This sentinel test just ensures every kind is reachable.
func TestRenderers_AllKindsPresent(t *testing.T) {
	for _, k := range AllKinds {
		assert.NotNilf(t, renderers[k], "renderer for %s missing", k)
	}
}

func TestRender_Welcome(t *testing.T) {
	subject, body, err := Render(KindWelcome, Vars{FirstName: "Sam"})
	require.NoError(t, err)
	assert.Contains(t, subject, "Welcome")
	assert.Contains(t, body, "Sam")
	assert.Contains(t, body, "<!DOCTYPE html>", "layout should be applied")
	assert.Contains(t, body, "footer", "layout footer should be applied")
}

func TestRender_TrialStarted_IncludesTrialEnd(t *testing.T) {
	_, body, err := Render(KindTrialStarted, Vars{
		FirstName:   "Alex",
		TrialEndsAt: "June 8, 2026",
		ProductName: "monthly subscription",
	})
	require.NoError(t, err)
	assert.Contains(t, body, "Alex")
	assert.Contains(t, body, "June 8, 2026")
}

func TestRender_TrialConverted(t *testing.T) {
	_, body, err := Render(KindTrialConverted, Vars{
		FirstName:       "Jamie",
		ProductName:     "annual subscription",
		NextRenewalDate: "June 1, 2027",
	})
	require.NoError(t, err)
	assert.Contains(t, body, "Jamie")
	assert.Contains(t, body, "annual subscription")
	assert.Contains(t, body, "June 1, 2027")
}

func TestRender_Cancelled_IncludesResubscribe(t *testing.T) {
	_, body, err := Render(KindCancelled, Vars{
		FirstName:      "Pat",
		AccessUntil:    "June 30, 2026",
		ResubscribeURL: "https://example.com/resub",
	})
	require.NoError(t, err)
	assert.Contains(t, body, "Pat")
	assert.Contains(t, body, "June 30, 2026")
	assert.Contains(t, body, "https://example.com/resub")
}

func TestRender_Expired(t *testing.T) {
	_, body, err := Render(KindExpired, Vars{
		FirstName:      "Robin",
		ResubscribeURL: "https://example.com/resub",
	})
	require.NoError(t, err)
	assert.Contains(t, body, "https://example.com/resub")
}

func TestRender_BillingIssue(t *testing.T) {
	_, body, err := Render(KindBillingIssue, Vars{
		FirstName:             "Casey",
		ManageSubscriptionURL: "https://example.com/manage",
	})
	require.NoError(t, err)
	assert.Contains(t, body, "Casey")
	assert.Contains(t, body, "https://example.com/manage")
}

func TestRender_UnknownKind(t *testing.T) {
	_, _, err := Render("nope", Vars{})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown kind"))
}

func TestRender_EmptyFirstName_StillWorks(t *testing.T) {
	// The "Hi {{if .FirstName}}{{.FirstName}}{{end}}" conditionals should
	// degrade gracefully when no name is available.
	_, body, err := Render(KindWelcome, Vars{})
	require.NoError(t, err)
	assert.NotEmpty(t, body)
}
