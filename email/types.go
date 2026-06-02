// Package email is the backend's transactional email layer. Templates live in
// email/templates as Go html/template files; sends go through Resend with
// exponential backoff; every attempt is recorded in public.email_log for
// audit, dedupe and debugging.
//
// Callers do not interact with Resend directly - they go through Send(ctx,
// userID, kind, vars), which loads the user, renders the matching template,
// dispatches with retry, and logs the outcome.
package email

import "fmt"

// EmailKind identifies which template to render and is recorded verbatim in
// email_log.kind for filtering and dedupe.
type EmailKind string

const (
	// KindWelcome - sent once when a user first signs up.
	KindWelcome EmailKind = "welcome"

	// KindTrialStarted - sent when an INITIAL_PURCHASE arrives with period_type=TRIAL.
	KindTrialStarted EmailKind = "trial_started"

	// KindTrialConverted - sent when a RENEWAL arrives with is_trial_conversion=true,
	// or whenever the user moves from status=trialing to status=active.
	KindTrialConverted EmailKind = "trial_converted"

	// KindCancelled - sent when the user toggles auto-renew off (CANCELLATION
	// event). They still have access until current_period_end.
	KindCancelled EmailKind = "cancelled"

	// KindExpired - sent when access actually ends (EXPIRATION). This is the
	// win-back moment.
	KindExpired EmailKind = "expired"

	// KindBillingIssue - sent when status flips to in_grace (BILLING_ISSUE).
	// Includes a deep link to manage payment method.
	KindBillingIssue EmailKind = "billing_issue"
)

// AllKinds is the canonical ordered list, used for iteration in tests.
var AllKinds = []EmailKind{
	KindWelcome,
	KindTrialStarted,
	KindTrialConverted,
	KindCancelled,
	KindExpired,
	KindBillingIssue,
}

// TemplateFile returns the basename of the html/template file for a kind.
// Kept in one place so adding a new kind is "add constant, add file" and
// nothing else.
func (k EmailKind) TemplateFile() string {
	return fmt.Sprintf("%s.html", k)
}

// SubjectFallback returns a default subject line in case the template
// itself does not define one via the {{define "subject"}} block. Marketing
// will override these in the templates; these stay as a defensive fallback
// so a forgotten subject never blocks a send.
func (k EmailKind) SubjectFallback() string {
	switch k {
	case KindWelcome:
		return "Welcome to Dead Reckoner"
	case KindTrialStarted:
		return "Your Pro trial has started"
	case KindTrialConverted:
		return "Welcome to Dead Reckoner Pro"
	case KindCancelled:
		return "Your subscription has been cancelled"
	case KindExpired:
		return "Your Dead Reckoner Pro subscription has ended"
	case KindBillingIssue:
		return "There is a problem with your subscription billing"
	}
	return "A message from Dead Reckoner"
}

// Status mirrors the values written to email_log.status.
type Status string

const (
	StatusSent    Status = "sent"
	StatusError   Status = "error"
	StatusSkipped Status = "skipped"
)
