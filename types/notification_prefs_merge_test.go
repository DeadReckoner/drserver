package types

import "testing"

// These guard the carry-over wiring: the new pref keys (agent_updates, nudges,
// messages), the shared-toggle Lookup aliases, and merge-on-read backfill that
// keeps existing users from being silently opted out of newly-added types.

// A JSONB blob written before agent_updates existed — every prior key present,
// the new one absent. This is exactly what an existing user's row looks like.
const legacyMyGoalsJSON = `{
  "daily_task_reminder":       {"enabled": true,  "delivery": "immediate"},
  "task_due_reminder":         {"enabled": true,  "delivery": "immediate"},
  "missed_task":               {"enabled": false, "delivery": "digest"},
  "falling_behind":            {"enabled": true,  "delivery": "digest"},
  "milestone_due":             {"enabled": true,  "delivery": "immediate"},
  "milestone_at_risk":         {"enabled": true,  "delivery": "immediate"},
  "follow_on_milestone_ready": {"enabled": true,  "delivery": "immediate"},
  "goal_milestone_completed":  {"enabled": true,  "delivery": "immediate"}
}`

const legacyFAndAJSON = `{
  "friend_request":          {"enabled": true,  "delivery": "immediate"},
  "anchor_request":          {"enabled": true,  "delivery": "immediate"},
  "anchor_accepted_removed": {"enabled": true,  "delivery": "immediate"},
  "missed_progress":         {"enabled": true,  "delivery": "digest"},
  "falling_behind":          {"enabled": true,  "delivery": "digest"},
  "milestone_at_risk":       {"enabled": true,  "delivery": "immediate"},
  "milestone_completed":     {"enabled": true,  "delivery": "digest"},
  "goal_completed":          {"enabled": true,  "delivery": "immediate"},
  "timeline_updated":        {"enabled": true,  "delivery": "digest"}
}`

func TestMyGoalsPrefs_ScanBackfillsMissingKey(t *testing.T) {
	var p MyGoalsPrefs
	if err := p.Scan([]byte(legacyMyGoalsJSON)); err != nil {
		t.Fatalf("Scan(): %v", err)
	}

	// The absent key is filled from defaults rather than left zero-valued
	// (which would read as enabled=false, silently muting the new type).
	want := DefaultMyGoalsPrefs().AgentUpdates
	if p.AgentUpdates != want {
		t.Errorf("AgentUpdates after backfill = %+v, want %+v (the default)", p.AgentUpdates, want)
	}

	// A key the user explicitly disabled must NOT be clobbered by the merge.
	if p.MissedTask.Enabled {
		t.Error("MissedTask was stored disabled; merge must not re-enable it")
	}
}

func TestFAndAPrefs_ScanBackfillsMissingKeys(t *testing.T) {
	var p FriendsAndAccountabilityPrefs
	if err := p.Scan([]byte(legacyFAndAJSON)); err != nil {
		t.Fatalf("Scan(): %v", err)
	}

	d := DefaultFAndAPrefs()
	if p.Nudges != d.Nudges {
		t.Errorf("Nudges after backfill = %+v, want %+v", p.Nudges, d.Nudges)
	}
	if p.Messages != d.Messages {
		t.Errorf("Messages after backfill = %+v, want %+v", p.Messages, d.Messages)
	}
}

func TestMyGoalsPrefs_LookupAgentUpdatesAliases(t *testing.T) {
	p := DefaultMyGoalsPrefs()
	for _, typ := range []string{
		"roadmap_complete", "roadmap_failed",
		"next_milestone_complete", "next_milestone_failed",
		"agent_updates",
	} {
		got, ok := p.Lookup(typ)
		if !ok {
			t.Errorf("Lookup(%q) not found; resolver would drop it as unknown type", typ)
			continue
		}
		if got != p.AgentUpdates {
			t.Errorf("Lookup(%q) = %+v, want the AgentUpdates pref %+v", typ, got, p.AgentUpdates)
		}
	}
}

func TestFAndAPrefs_LookupSharedAndNewAliases(t *testing.T) {
	p := DefaultFAndAPrefs()
	cases := []struct {
		typ  string
		want NotificationTypePref
	}{
		{"friend_accepted", p.FriendRequest},
		{"friend_rejected", p.FriendRequest},
		{"anchor_rejected", p.AnchorAcceptedRemoved},
		{"habit_anchor_request", p.AnchorRequest},
		{"roadmap_nudge", p.Nudges},
		{"habit_nudge", p.Nudges},
		{"nudges", p.Nudges},
		{"message_new", p.Messages},
		{"messages", p.Messages},
	}
	for _, c := range cases {
		got, ok := p.Lookup(c.typ)
		if !ok {
			t.Errorf("Lookup(%q) not found; resolver would drop it as unknown type", c.typ)
			continue
		}
		if got != c.want {
			t.Errorf("Lookup(%q) = %+v, want %+v", c.typ, got, c.want)
		}
	}
}

// An unknown type still returns false — the whitelist must not become a
// pass-through just because we added aliases.
func TestFAndAPrefs_LookupUnknownStillFalse(t *testing.T) {
	p := DefaultFAndAPrefs()
	if _, ok := p.Lookup("totally_made_up"); ok {
		t.Error("Lookup of unknown type returned ok=true; whitelist breached")
	}
}
