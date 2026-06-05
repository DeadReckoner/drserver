package types

import (
	"reflect"
	"testing"
)

// These tests round-trip the JSONB-backed types through Value() and Scan()
// to catch Scanner/Valuer implementation errors without needing a database.

func TestMyGoalsPrefs_RoundTrip(t *testing.T) {
	original := DefaultMyGoalsPrefs()

	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value(): %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("Value() returned %T, want string", v)
	}

	var restored MyGoalsPrefs
	if err := restored.Scan([]byte(s)); err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Errorf("round-trip mismatch:\n  before: %+v\n  after:  %+v", original, restored)
	}
}

func TestFriendsAndAccountabilityPrefs_RoundTrip(t *testing.T) {
	original := DefaultFAndAPrefs()

	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value(): %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("Value() returned %T, want string", v)
	}

	var restored FriendsAndAccountabilityPrefs
	if err := restored.Scan([]byte(s)); err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Errorf("round-trip mismatch:\n  before: %+v\n  after:  %+v", original, restored)
	}
}

func TestPrefOverrides_RoundTrip(t *testing.T) {
	original := PrefOverrides{
		"missed_progress": {Enabled: false, Delivery: DeliveryDigest},
		"falling_behind":  {Enabled: true, Delivery: DeliveryImmediate},
	}

	v, err := original.Value()
	if err != nil {
		t.Fatalf("Value(): %v", err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("Value() returned %T, want string", v)
	}

	var restored PrefOverrides
	if err := restored.Scan([]byte(s)); err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Errorf("round-trip mismatch:\n  before: %+v\n  after:  %+v", original, restored)
	}
}

// Empty/nil cases — Scan(nil) and a nil PrefOverrides should produce usable values.
func TestPrefOverrides_NilHandling(t *testing.T) {
	// Nil map should serialize as empty object.
	var p PrefOverrides
	v, err := p.Value()
	if err != nil {
		t.Fatalf("Value() on nil: %v", err)
	}
	if v.(string) != "{}" {
		t.Errorf("Value() on nil = %q, want %q", v.(string), "{}")
	}

	// Scan(nil) should produce an empty (non-nil) map.
	var q PrefOverrides
	if err := q.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if q == nil {
		t.Error("Scan(nil) produced nil map, want empty map")
	}
}

// Defaults sanity: every notification type should be enabled by default.
// If this changes, update the SQL DEFAULT in
// supabase/migrations/20260518000001_add_notification_settings_tables.sql.
func TestDefaultMyGoalsPrefs_AllEnabled(t *testing.T) {
	d := DefaultMyGoalsPrefs()
	cases := map[string]NotificationTypePref{
		"DailyTaskReminder":      d.DailyTaskReminder,
		"TaskDueReminder":        d.TaskDueReminder,
		"MissedTask":             d.MissedTask,
		"FallingBehind":          d.FallingBehind,
		"MilestoneDue":           d.MilestoneDue,
		"MilestoneAtRisk":        d.MilestoneAtRisk,
		"FollowOnMilestoneReady": d.FollowOnMilestoneReady,
		"GoalMilestoneCompleted": d.GoalMilestoneCompleted,
	}
	for name, p := range cases {
		if !p.Enabled {
			t.Errorf("MyGoalsPrefs.%s should default to enabled", name)
		}
		if p.Delivery != DeliveryImmediate && p.Delivery != DeliveryDigest {
			t.Errorf("MyGoalsPrefs.%s.Delivery = %q, want immediate or digest", name, p.Delivery)
		}
	}
}

func TestDefaultFAndAPrefs_AllEnabled(t *testing.T) {
	d := DefaultFAndAPrefs()
	cases := map[string]NotificationTypePref{
		"FriendRequest":         d.FriendRequest,
		"AnchorRequest":         d.AnchorRequest,
		"AnchorAcceptedRemoved": d.AnchorAcceptedRemoved,
		"MissedProgress":        d.MissedProgress,
		"FallingBehind":         d.FallingBehind,
		"MilestoneAtRisk":       d.MilestoneAtRisk,
		"MilestoneCompleted":    d.MilestoneCompleted,
		"GoalCompleted":         d.GoalCompleted,
		"TimelineUpdated":       d.TimelineUpdated,
	}
	for name, p := range cases {
		if !p.Enabled {
			t.Errorf("FAndAPrefs.%s should default to enabled", name)
		}
		if p.Delivery != DeliveryImmediate && p.Delivery != DeliveryDigest {
			t.Errorf("FAndAPrefs.%s.Delivery = %q, want immediate or digest", name, p.Delivery)
		}
	}
}
