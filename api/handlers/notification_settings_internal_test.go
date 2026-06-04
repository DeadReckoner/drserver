package handlers

import (
	"encoding/json"
	"reflect"
	"testing"

	"dr/server/types"
)

// TestUpdateRequest_toMap_SerializesJSONBToString is a regression guard for
// the SQLSTATE 22P02 bug: putting a typed struct value into map[string]any
// and then handing the map to GORM's Updates() bypasses the struct's
// Value() method, so Postgres receives Go's default `%v` formatting of the
// struct ("{Enabled:true Delivery:immediate}"), rejects it as invalid JSON,
// and the whole UPDATE fails.
//
// The fix is in toMap(): pre-marshal MyGoalsPrefs and FAndAPrefs to a JSON
// string before stuffing them in the map. Postgres casts string -> jsonb
// natively, so this is robust across drivers.
//
// If you change toMap() and the JSONB columns end up as raw struct values
// in the map again, this test catches it before it ever ships.
func TestUpdateRequest_toMap_SerializesJSONBToString(t *testing.T) {
	my := types.DefaultMyGoalsPrefs()
	fa := types.DefaultFAndAPrefs()
	req := UpdateNotificationSettingsRequest{
		MyGoalsPrefs: &my,
		FAndAPrefs:   &fa,
	}
	m := req.toMap()

	t.Run("my_goals_prefs", func(t *testing.T) {
		v, ok := m["my_goals_prefs"]
		if !ok {
			t.Fatal("missing my_goals_prefs in toMap output")
		}
		s, isStr := v.(string)
		if !isStr {
			t.Fatalf("my_goals_prefs has Go type %T, want string. Putting a raw struct in the map breaks GORM's UPDATE for jsonb columns (SQLSTATE 22P02).", v)
		}
		// Bytes must round-trip back to the original struct.
		var roundtrip types.MyGoalsPrefs
		if err := json.Unmarshal([]byte(s), &roundtrip); err != nil {
			t.Fatalf("my_goals_prefs not valid JSON: %v\nvalue: %s", err, s)
		}
		if !reflect.DeepEqual(roundtrip, my) {
			t.Errorf("MyGoalsPrefs round-trip mismatch:\n  before: %+v\n  after:  %+v", my, roundtrip)
		}
	})

	t.Run("f_and_a_prefs", func(t *testing.T) {
		v, ok := m["f_and_a_prefs"]
		if !ok {
			t.Fatal("missing f_and_a_prefs in toMap output")
		}
		s, isStr := v.(string)
		if !isStr {
			t.Fatalf("f_and_a_prefs has Go type %T, want string.", v)
		}
		var roundtrip types.FriendsAndAccountabilityPrefs
		if err := json.Unmarshal([]byte(s), &roundtrip); err != nil {
			t.Fatalf("f_and_a_prefs not valid JSON: %v\nvalue: %s", err, s)
		}
		if !reflect.DeepEqual(roundtrip, fa) {
			t.Errorf("FAndAPrefs round-trip mismatch:\n  before: %+v\n  after:  %+v", fa, roundtrip)
		}
	})
}

// TestUpdateRequest_toMap_OmitsAbsentJSONBFields confirms that JSONB fields
// the caller didn't set are absent from the map entirely. They must NOT be
// included as empty strings or null, since GORM would otherwise overwrite
// the column with a garbage value.
func TestUpdateRequest_toMap_OmitsAbsentJSONBFields(t *testing.T) {
	req := UpdateNotificationSettingsRequest{}
	m := req.toMap()
	if _, ok := m["my_goals_prefs"]; ok {
		t.Errorf("my_goals_prefs included when request didn't set it; value=%v", m["my_goals_prefs"])
	}
	if _, ok := m["f_and_a_prefs"]; ok {
		t.Errorf("f_and_a_prefs included when request didn't set it; value=%v", m["f_and_a_prefs"])
	}
}

// TestUpdateRequest_toMap_PassesScalarFieldsThrough is a sanity check that
// the fix didn't accidentally break the other (non-JSONB) fields. They
// should still land in the map with their original Go types — bools stay
// bools (so GORM writes `false` properly), DigestFrequency stays its named
// string type, etc.
func TestUpdateRequest_toMap_PassesScalarFieldsThrough(t *testing.T) {
	push := false
	digestFreq := types.DigestFrequencyDaily
	req := UpdateNotificationSettingsRequest{
		PushEnabled:     &push,
		DigestFrequency: &digestFreq,
	}
	m := req.toMap()

	if v, ok := m["push_enabled"].(bool); !ok || v != false {
		t.Errorf("push_enabled = %v (type %T), want false (bool)", m["push_enabled"], m["push_enabled"])
	}
	if v, ok := m["digest_frequency"].(types.DigestFrequency); !ok || v != types.DigestFrequencyDaily {
		t.Errorf("digest_frequency = %v (type %T), want %q", m["digest_frequency"], m["digest_frequency"], types.DigestFrequencyDaily)
	}
}
