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

func TestRender_ReportReceived_User(t *testing.T) {
	subject, body, err := Render(KindReportReceived, Vars{
		ReportID:         "rep-1",
		ReportType:       "user",
		ReportReason:     "harassment",
		ReporterName:     "Sam Reporter",
		ReporterID:       "user-1",
		ReportedUserName: "Bad Actor",
		ReportedUserID:   "user-2",
		ReportNote:       "they sent abusive messages",
	})
	require.NoError(t, err)
	assert.Contains(t, subject, "user")
	assert.Contains(t, subject, "harassment")
	assert.Contains(t, body, "Bad Actor")
	assert.Contains(t, body, "they sent abusive messages")
	assert.Contains(t, body, "<!DOCTYPE html>", "layout should be applied")
}

func TestRender_ReportReceived_AI(t *testing.T) {
	subject, body, err := Render(KindReportReceived, Vars{
		ReportID:         "rep-2",
		ReportType:       "ai",
		ReportReason:     "unsafe",
		ReporterID:       "user-1",
		ReportOutputType: "roadmap",
		ReportContext:    "AI suggested an unsafe fasting plan",
	})
	require.NoError(t, err)
	assert.Contains(t, subject, "AI content")
	assert.Contains(t, body, "roadmap")
	assert.Contains(t, body, "AI suggested an unsafe fasting plan")
}

func TestRender_FeedbackReceived_Support(t *testing.T) {
	subject, body, err := Render(KindFeedbackReceived, Vars{
		FeedbackID:       "fb-1",
		FeedbackCategory: "support",
		FeedbackSubject:  "Cannot sync my roadmap",
		FeedbackMessage:  "The sync spinner never stops on the goals tab.",
		SubmitterName:    "Sam Submitter",
		SubmitterID:      "user-1",
		SubmitterEmail:   "sam@example.com",
		FeedbackAppVer:   "1.4.2",
		FeedbackOS:       "iOS 18.2",
		FeedbackDevice:   "iPhone 16 Pro",
	})
	require.NoError(t, err)
	assert.Contains(t, subject, "Support request")
	assert.Contains(t, subject, "Cannot sync my roadmap")
	assert.Contains(t, body, "The sync spinner never stops on the goals tab.")
	assert.Contains(t, body, "sam@example.com")
	assert.Contains(t, body, "iPhone 16 Pro")
	assert.Contains(t, body, "<!DOCTYPE html>", "layout should be applied")
}

func TestRender_FeedbackReceived_BugWithoutSubject(t *testing.T) {
	subject, body, err := Render(KindFeedbackReceived, Vars{
		FeedbackID:       "fb-2",
		FeedbackCategory: "bug",
		FeedbackMessage:  "Crash on opening settings.",
		SubmitterName:    "Sam Submitter",
		SubmitterID:      "user-1",
	})
	require.NoError(t, err)
	// No subject line collected for non-support categories, so the submitter
	// name carries the subject instead of leaving it dangling.
	assert.Contains(t, subject, "Bug report")
	assert.Contains(t, subject, "Sam Submitter")
	assert.Contains(t, body, "Crash on opening settings.")
	assert.NotContains(t, body, "<strong>Subject:</strong>")
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
