package jobs

import (
	"context"
	"errors"
	"testing"

	"dr/server/email"
	"dr/server/types"
)

// fakeEmailSender records what the cron asked email to send.
type fakeEmailSender struct {
	calls []emailCall
	err   error
}

type emailCall struct {
	UserID    string
	Kind      email.EmailKind
	Vars      email.Vars
	DedupeKey string
}

func (f *fakeEmailSender) Send(_ context.Context, userID string, kind email.EmailKind, vars email.Vars, dedupeKey string) error {
	f.calls = append(f.calls, emailCall{userID, kind, vars, dedupeKey})
	return f.err
}

// =============================================================================
// deliver_digests
// =============================================================================
// The candidate query is exercised in integration tests against a real
// Postgres. These tests inject candidates + items + delete hooks to
// exercise the cron's send/skip/delete branch logic directly.

func TestDeliverDueDigests_SendsDailyAndWeekly(t *testing.T) {
	candidates := []digestCandidate{
		{UserID: "user-1", FirstName: "Alex", DigestFrequency: "daily"},
		{UserID: "user-2", FirstName: "", DigestFrequency: "weekly"},
	}
	items := map[string][]types.DigestItem{
		"user-1": {
			{Title: "Missed task", Body: "Write the README"},
			{Title: "Falling behind", Body: "You're 5 tasks behind"},
		},
		"user-2": {
			{Title: "Weekly summary", Body: "12 tasks completed"},
		},
	}

	sender := &fakeEmailSender{}
	deleted := map[string]int{}

	job := deliverDueDigestsWith(
		func() ([]digestCandidate, error) { return candidates, nil },
		sender,
		func(userID string) ([]types.DigestItem, error) { return items[userID], nil },
		func(userID string) (int64, error) {
			deleted[userID]++
			return int64(len(items[userID])), nil
		},
	)
	job()

	if len(sender.calls) != 2 {
		t.Fatalf("expected 2 email sends, got %d", len(sender.calls))
	}
	if sender.calls[0].Kind != email.KindDailyDigest {
		t.Errorf("first call kind = %q, want daily_digest", sender.calls[0].Kind)
	}
	if sender.calls[1].Kind != email.KindWeeklyDigest {
		t.Errorf("second call kind = %q, want weekly_digest", sender.calls[1].Kind)
	}
	if got := len(sender.calls[0].Vars.DigestItems); got != 2 {
		t.Errorf("user-1 vars had %d items, want 2", got)
	}
	if got := len(sender.calls[1].Vars.DigestItems); got != 1 {
		t.Errorf("user-2 vars had %d items, want 1", got)
	}
	if sender.calls[0].Vars.FirstName != "Alex" {
		t.Errorf("first call FirstName = %q, want Alex", sender.calls[0].Vars.FirstName)
	}
	if deleted["user-1"] != 1 || deleted["user-2"] != 1 {
		t.Errorf("delete not called per user: %v", deleted)
	}
}

func TestDeliverDueDigests_SilentSkipOnEmpty(t *testing.T) {
	// Per the product decision: a candidate with zero items must NOT
	// receive an "all clear" email; the cron just skips silently.
	candidates := []digestCandidate{
		{UserID: "user-1", FirstName: "Alex", DigestFrequency: "daily"},
	}

	sender := &fakeEmailSender{}
	deleted := 0

	job := deliverDueDigestsWith(
		func() ([]digestCandidate, error) { return candidates, nil },
		sender,
		func(userID string) ([]types.DigestItem, error) { return nil, nil },
		func(userID string) (int64, error) { deleted++; return 0, nil },
	)
	job()

	if len(sender.calls) != 0 {
		t.Errorf("email sent despite empty items: %+v", sender.calls)
	}
	if deleted != 0 {
		t.Errorf("delete called %d times on empty path, want 0", deleted)
	}
}

func TestDeliverDueDigests_SendFailureSkipsDelete(t *testing.T) {
	// If the email send fails, we MUST NOT delete the items — they need
	// to ride along in the next digest (or get retried at the next tick
	// after the 23h email_log window expires). Test that delete isn't
	// called when Send returns an error.
	candidates := []digestCandidate{
		{UserID: "user-1", FirstName: "Alex", DigestFrequency: "daily"},
	}
	items := []types.DigestItem{{Title: "T", Body: "B"}}

	sender := &fakeEmailSender{err: errors.New("resend down")}
	deleted := 0

	job := deliverDueDigestsWith(
		func() ([]digestCandidate, error) { return candidates, nil },
		sender,
		func(string) ([]types.DigestItem, error) { return items, nil },
		func(string) (int64, error) { deleted++; return 0, nil },
	)
	job()

	if len(sender.calls) != 1 {
		t.Errorf("expected 1 send attempt, got %d", len(sender.calls))
	}
	if deleted != 0 {
		t.Errorf("delete called despite send failure (would orphan the user's items): %d times", deleted)
	}
}

func TestDeliverDueDigests_CandidateQueryErrorSwallowed(t *testing.T) {
	sender := &fakeEmailSender{}
	job := deliverDueDigestsWith(
		func() ([]digestCandidate, error) { return nil, errors.New("db down") },
		sender,
		func(string) ([]types.DigestItem, error) { return nil, nil },
		func(string) (int64, error) { return 0, nil },
	)
	// Must not panic.
	job()
	if len(sender.calls) != 0 {
		t.Errorf("sent on error path: %d", len(sender.calls))
	}
}
