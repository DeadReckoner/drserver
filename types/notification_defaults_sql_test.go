package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

// The notification-settings JSONB columns have a default blob in SQL that MUST
// stay byte-for-byte equivalent to the Go DefaultMyGoalsPrefs()/DefaultFAndAPrefs()
// functions. Every prior step has guarded this by hand ("if you change one,
// change the other"); this test makes the invariant executable.
//
// It reads the migrations off disk, extracts the *latest* DEFAULT assigned to
// each column (covering both the original CREATE TABLE default and any later
// ALTER COLUMN ... SET DEFAULT), and compares it to the Go default. Comparison
// is done as maps so a key present in one side but not the other fails the test
// — not just value drift on shared keys.

// defaultBlobRe captures the JSON object in `<column> ... default '{...}'::jsonb`.
// The [^']*? between the column name and `default` cannot cross a single quote,
// so it matches the column-definition and ALTER...SET DEFAULT forms but NOT the
// backfill UPDATEs (`set <column> = '{...}'`), whose value-quote comes first.
func defaultBlobRe(column string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)` + regexp.QuoteMeta(column) + `"?[^']*?default\s+'(\{[^']*\})'::jsonb`)
}

// extractLatestSQLDefault returns the JSON text of the most recent DEFAULT for
// the column across all migration files, in chronological (filename-sorted)
// order. Empty string if none found.
func extractLatestSQLDefault(t *testing.T, column string) string {
	t.Helper()
	files, err := filepath.Glob("../supabase/migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migration files found under ../supabase/migrations")
	}
	sort.Strings(files)

	re := defaultBlobRe(column)
	var latest string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			latest = m[1]
		}
	}
	return latest
}

// asMap normalizes either side to a map[string]NotificationTypePref so the
// comparison is order- and representation-independent and surfaces missing or
// extra keys.
func asMap(t *testing.T, v any) map[string]NotificationTypePref {
	t.Helper()
	var b []byte
	var err error
	switch x := v.(type) {
	case string:
		b = []byte(x)
	default:
		b, err = json.Marshal(x)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
	}
	m := map[string]NotificationTypePref{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal to map: %v\nblob: %s", err, string(b))
	}
	return m
}

func TestSQLDefault_MyGoalsPrefs_MatchesGo(t *testing.T) {
	sql := extractLatestSQLDefault(t, "my_goals_prefs")
	if sql == "" {
		t.Fatal("no SQL default found for my_goals_prefs")
	}
	want := asMap(t, DefaultMyGoalsPrefs())
	got := asMap(t, sql)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("my_goals_prefs SQL default != DefaultMyGoalsPrefs()\n  go:  %+v\n  sql: %+v", want, got)
	}
}

func TestSQLDefault_FAndAPrefs_MatchesGo(t *testing.T) {
	sql := extractLatestSQLDefault(t, "f_and_a_prefs")
	if sql == "" {
		t.Fatal("no SQL default found for f_and_a_prefs")
	}
	want := asMap(t, DefaultFAndAPrefs())
	got := asMap(t, sql)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("f_and_a_prefs SQL default != DefaultFAndAPrefs()\n  go:  %+v\n  sql: %+v", want, got)
	}
}
