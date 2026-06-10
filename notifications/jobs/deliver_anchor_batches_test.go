package jobs

import (
	"strings"
	"testing"

	"dr/server/email"
)

// =============================================================================
// summaryLine — pure formula on the per-anchored-user counters.
// =============================================================================

func TestSummaryLine_PrioritizesMissedThenOverdueThenCompleted(t *testing.T) {
	cases := []struct {
		name   string
		row    anchoredSummaryRow
		expect string // substring the result must contain, or "" for empty
	}{
		{"≥3 missed wins over everything", anchoredSummaryRow{MissedIn7d: 5, OverdueNow: 1, CompletedIn7d: 3}, "missed 5"},
		{"exactly 3 missed trips", anchoredSummaryRow{MissedIn7d: 3}, "missed 3"},
		{"2 missed falls through to overdue", anchoredSummaryRow{MissedIn7d: 2, OverdueNow: 1}, "overdue"},
		{"only overdue", anchoredSummaryRow{OverdueNow: 4}, "overdue task(s)"},
		{"no problems but completed work", anchoredSummaryRow{CompletedIn7d: 7}, "Completed 7"},
		{"silent if nothing", anchoredSummaryRow{}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summaryLine(tc.row)
			if tc.expect == "" {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.expect) {
				t.Errorf("got %q, want substring %q", got, tc.expect)
			}
		})
	}
}

// =============================================================================
// deliver_anchor_batches
// =============================================================================

func TestDeliverAnchorBatches_GroupsSummariesAndSends(t *testing.T) {
	// One anchor with two anchored people, one of whom is silent (empty
	// summaryLine). Verify the silent person is dropped from the email.
	candidates := []anchorCandidate{
		{AnchorUserID: "anchor-1", AnchorFirstName: "Sam"},
	}
	summaries := []anchoredSummaryRow{
		{AnchorUserID: "anchor-1", AnchoredName: "Mike", MissedIn7d: 4},
		{AnchorUserID: "anchor-1", AnchoredName: "Pat"}, // silent
		{AnchorUserID: "anchor-1", AnchoredName: "Jess", CompletedIn7d: 5},
	}

	sender := &fakeEmailSender{}
	job := deliverAnchorBatchesWith(
		func() ([]anchorCandidate, error) { return candidates, nil },
		func([]string) ([]anchoredSummaryRow, error) { return summaries, nil },
		sender,
	)
	job()

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 send, got %d", len(sender.calls))
	}
	call := sender.calls[0]
	if call.Kind != email.KindAnchorDailySummary {
		t.Errorf("kind = %q, want anchor_daily_summary", call.Kind)
	}
	if call.Vars.FirstName != "Sam" {
		t.Errorf("FirstName = %q, want Sam", call.Vars.FirstName)
	}
	if len(call.Vars.AnchoredUsers) != 2 {
		t.Fatalf("expected 2 AnchoredUsers (silent Pat dropped), got %d: %+v", len(call.Vars.AnchoredUsers), call.Vars.AnchoredUsers)
	}
	names := []string{call.Vars.AnchoredUsers[0].Name, call.Vars.AnchoredUsers[1].Name}
	if names[0] != "Mike" || names[1] != "Jess" {
		t.Errorf("AnchoredUsers names = %v, want [Mike, Jess]", names)
	}
}

func TestDeliverAnchorBatches_SilentSkipWhenAllAnchoredAreQuiet(t *testing.T) {
	// Anchor exists, has anchored people, but all of them are quiet
	// (empty summaryLine). Per the product decision: no email.
	candidates := []anchorCandidate{
		{AnchorUserID: "anchor-1", AnchorFirstName: "Sam"},
	}
	summaries := []anchoredSummaryRow{
		{AnchorUserID: "anchor-1", AnchoredName: "Mike"}, // silent
		{AnchorUserID: "anchor-1", AnchoredName: "Pat"},  // silent
	}

	sender := &fakeEmailSender{}
	job := deliverAnchorBatchesWith(
		func() ([]anchorCandidate, error) { return candidates, nil },
		func([]string) ([]anchoredSummaryRow, error) { return summaries, nil },
		sender,
	)
	job()

	if len(sender.calls) != 0 {
		t.Errorf("expected silent skip, got %d sends: %+v", len(sender.calls), sender.calls)
	}
}

func TestDeliverAnchorBatches_NoCandidatesShortCircuits(t *testing.T) {
	summariesCalled := 0
	sender := &fakeEmailSender{}
	job := deliverAnchorBatchesWith(
		func() ([]anchorCandidate, error) { return nil, nil },
		func([]string) ([]anchoredSummaryRow, error) { summariesCalled++; return nil, nil },
		sender,
	)
	job()
	if summariesCalled != 0 {
		t.Errorf("summaries query called despite zero candidates: %d times", summariesCalled)
	}
}
