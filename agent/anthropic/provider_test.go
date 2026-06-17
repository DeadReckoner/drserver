package anthropic

import "testing"

// TestParseMilestoneTasks_Dependencies verifies that the dependencies field is
// normalized into the *string (JSON-array literal) shape that
// types.Task.Dependencies expects, regardless of whether the model returns it as
// a JSON array (Claude's tendency) or an already-encoded string (OpenAI's).
func TestParseMilestoneTasks_Dependencies(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantDeps *string
	}{
		{
			name:     "array form (the failing case)",
			input:    `{"tasks":[{"title":"Build API","dependencies":["Install Go","Configure IDE"]}]}`,
			wantDeps: ptr(`["Install Go","Configure IDE"]`),
		},
		{
			name:     "array with whitespace is compacted",
			input:    `{"tasks":[{"title":"Build API","dependencies":["A", "B"]}]}`,
			wantDeps: ptr(`["A","B"]`),
		},
		{
			name:     "empty array",
			input:    `{"tasks":[{"title":"Build API","dependencies":[]}]}`,
			wantDeps: ptr(`[]`),
		},
		{
			name:     "string form is passed through",
			input:    `{"tasks":[{"title":"Build API","dependencies":"[\"A\"]"}]}`,
			wantDeps: ptr(`["A"]`),
		},
		{
			name:     "absent dependencies stays nil",
			input:    `{"tasks":[{"title":"Build API"}]}`,
			wantDeps: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := parseMilestoneTasks([]byte(tc.input))
			if err != nil {
				t.Fatalf("parseMilestoneTasks returned error: %v", err)
			}
			if len(tasks) != 1 {
				t.Fatalf("expected 1 task, got %d", len(tasks))
			}
			got := tasks[0].Dependencies
			switch {
			case tc.wantDeps == nil && got != nil:
				t.Fatalf("expected nil dependencies, got %q", *got)
			case tc.wantDeps != nil && got == nil:
				t.Fatalf("expected dependencies %q, got nil", *tc.wantDeps)
			case tc.wantDeps != nil && got != nil && *got != *tc.wantDeps:
				t.Fatalf("dependencies mismatch: got %q, want %q", *got, *tc.wantDeps)
			}
		})
	}
}

func ptr(s string) *string { return &s }
