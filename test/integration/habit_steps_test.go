//go:build integration

package integration

import (
	"net/http/httptest"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/types"

	"github.com/gin-gonic/gin"
)

// Multi-step recurring habits/tasks, exercised against real Postgres with the
// production migration applied. These cover the semantics that unit tests with
// a mocked driver cannot: the unique indexes that make completion
// exactly-once, ON DELETE SET NULL preserving history, and the
// materialise-on-write / project-on-read split that makes each occurrence
// independent.

func stepCtx(userID string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set("user_id", userID)
	return c
}

func newHabit(t *testing.T, userID string) string {
	t.Helper()
	var id string
	err := testDB.Raw(
		`insert into public.habits (user_id, title, recurrence_type, start_date)
		 values (?, ?, 'daily', now() - interval '30 days') returning habit_id`,
		userID, "Morning routine",
	).Row().Scan(&id)
	if err != nil {
		t.Fatalf("newHabit: %v", err)
	}
	return id
}

func addStep(t *testing.T, c *gin.Context, habitID, desc string) types.HabitStep {
	t.Helper()
	s, err := database.CreateHabitStep(c, habitID, types.CreateHabitStepRequest{Description: desc})
	if err != nil {
		t.Fatalf("CreateHabitStep(%q): %v", desc, err)
	}
	return s
}

func completionCount(t *testing.T, habitID string, day time.Time) int {
	t.Helper()
	return countRows(t,
		`select count(*) from public.habit_completions where habit_id = ? and date = ?`,
		habitID, day.Format("2006-01-02"))
}

// --- Backward compatibility --------------------------------------------------

// A habit with no steps must behave exactly as it did before this feature:
// no checklist, and completion driven solely by the complete endpoint.
func TestHabitSteps_SteplessHabit_Unchanged(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	occ, err := database.GetOccurrenceSteps(c, habitID, time.Now())
	if err != nil {
		t.Fatalf("GetOccurrenceSteps: %v", err)
	}
	if occ.StepsTotal != 0 || len(occ.Steps) != 0 {
		t.Fatalf("stepless habit reported %d steps, want 0", occ.StepsTotal)
	}

	if _, err := database.CompleteHabit(c, habitID, time.Now(), ""); err != nil {
		t.Fatalf("CompleteHabit: %v", err)
	}
	if got := completionCount(t, habitID, time.Now()); got != 1 {
		t.Fatalf("completions=%d, want 1", got)
	}
}

// --- Creation and projection -------------------------------------------------

// An occurrence nobody has touched has no rows of its own; it is projected off
// the template as all-incomplete. That projection is what makes every future
// occurrence (and the next recurrence) start fresh with no reset job.
func TestHabitSteps_UntouchedOccurrenceProjectsTemplateAsIncomplete(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	addStep(t, c, habitID, "Boil water")
	addStep(t, c, habitID, "Grind beans")
	addStep(t, c, habitID, "Pour")

	occ, err := database.GetOccurrenceSteps(c, habitID, time.Now())
	if err != nil {
		t.Fatalf("GetOccurrenceSteps: %v", err)
	}
	if occ.StepsTotal != 3 || occ.StepsCompleted != 0 {
		t.Fatalf("got %d/%d steps, want 0/3", occ.StepsCompleted, occ.StepsTotal)
	}
	if occ.IsCompleted {
		t.Error("occurrence with no completed steps must not report complete")
	}
	if occ.Steps[0].Description != "Boil water" || occ.Steps[2].Description != "Pour" {
		t.Errorf("steps out of order: %+v", occ.Steps)
	}

	// Nothing materialised yet.
	if n := countRows(t, `select count(*) from public.habit_step_completions where habit_id = ?`, habitID); n != 0 {
		t.Errorf("materialised %d rows on read, want 0 (reads must stay side-effect free)", n)
	}
}

// --- Completion rules --------------------------------------------------------

func TestHabitSteps_PartialProgressDoesNotCompleteParent(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	addStep(t, c, habitID, "two")

	occ, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusCompleted))
	if err != nil {
		t.Fatalf("SetHabitStepState: %v", err)
	}
	if occ.StepsCompleted != 1 || occ.StepsTotal != 2 {
		t.Fatalf("progress=%d/%d, want 1/2", occ.StepsCompleted, occ.StepsTotal)
	}
	if occ.IsCompleted {
		t.Error("parent completed with a step still incomplete")
	}
	if got := completionCount(t, habitID, time.Now()); got != 0 {
		t.Fatalf("completions=%d, want 0", got)
	}
}

// Checking the last incomplete step is what completes the occurrence -- the
// same rule UpdateStep applies to roadmap tasks via shouldCompleteTask.
func TestHabitSteps_FinalStepCompletesParent(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	s2 := addStep(t, c, habitID, "two")

	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusCompleted)); err != nil {
		t.Fatalf("step 1: %v", err)
	}
	occ, err := database.SetHabitStepState(c, habitID, s2.HabitStepID, time.Now(), string(types.StepStatusCompleted))
	if err != nil {
		t.Fatalf("step 2: %v", err)
	}

	if !occ.IsCompleted {
		t.Error("parent not completed after final step")
	}
	if got := completionCount(t, habitID, time.Now()); got != 1 {
		t.Fatalf("completions=%d, want exactly 1", got)
	}
}

// "skipped" counts as done, matching shouldCompleteTask.
func TestHabitSteps_SkippedCountsAsDone(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	s2 := addStep(t, c, habitID, "two")

	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusCompleted)); err != nil {
		t.Fatalf("step 1: %v", err)
	}
	occ, err := database.SetHabitStepState(c, habitID, s2.HabitStepID, time.Now(), string(types.StepStatusSkipped))
	if err != nil {
		t.Fatalf("step 2 skip: %v", err)
	}
	if !occ.IsCompleted {
		t.Error("skipping the last step must complete the occurrence, as it does for roadmap steps")
	}
}

func TestHabitSteps_UncheckingReopensParent(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	s2 := addStep(t, c, habitID, "two")

	_, _ = database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusCompleted))
	_, _ = database.SetHabitStepState(c, habitID, s2.HabitStepID, time.Now(), string(types.StepStatusCompleted))
	if got := completionCount(t, habitID, time.Now()); got != 1 {
		t.Fatalf("setup: completions=%d, want 1", got)
	}

	occ, err := database.SetHabitStepState(c, habitID, s2.HabitStepID, time.Now(), string(types.StepStatusIncomplete))
	if err != nil {
		t.Fatalf("uncheck: %v", err)
	}
	if occ.IsCompleted {
		t.Error("parent still complete after unchecking a step")
	}
	if got := completionCount(t, habitID, time.Now()); got != 0 {
		t.Fatalf("completions=%d after reopen, want 0", got)
	}
}

// Rapid taps / retries / a second device must not multiply the completion.
// The (habit_id, date) unique index plus ON CONFLICT DO NOTHING is what
// guarantees it, so this asserts against the real constraint.
func TestHabitSteps_RepeatedFinalStepIsIdempotent(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "only step")

	for i := 0; i < 5; i++ {
		if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusCompleted)); err != nil {
			t.Fatalf("repeat %d: %v", i, err)
		}
	}

	if got := completionCount(t, habitID, time.Now()); got != 1 {
		t.Fatalf("completions=%d after 5 identical taps, want exactly 1", got)
	}
	if n := countRows(t,
		`select count(*) from public.habit_step_completions where habit_id = ? and date = ?`,
		habitID, time.Now().Format("2006-01-02")); n != 1 {
		t.Fatalf("step rows=%d, want 1 (no duplicate checklist entries)", n)
	}
}

// --- Recurrence independence -------------------------------------------------

// Completing today must not carry into tomorrow, and completing today must not
// retroactively touch yesterday.
func TestHabitSteps_NextOccurrenceStartsFresh(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	yesterday := time.Now().AddDate(0, 0, -1)

	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, yesterday, string(types.StepStatusCompleted)); err != nil {
		t.Fatalf("yesterday: %v", err)
	}

	prev, err := database.GetOccurrenceSteps(c, habitID, yesterday)
	if err != nil {
		t.Fatalf("read yesterday: %v", err)
	}
	if !prev.IsCompleted || prev.StepsCompleted != 1 {
		t.Fatalf("yesterday=%d/%d completed=%v, want 1/1 complete", prev.StepsCompleted, prev.StepsTotal, prev.IsCompleted)
	}

	today, err := database.GetOccurrenceSteps(c, habitID, time.Now())
	if err != nil {
		t.Fatalf("read today: %v", err)
	}
	if today.StepsCompleted != 0 || today.IsCompleted {
		t.Fatalf("today=%d/%d completed=%v, want 0/1 incomplete -- state leaked across occurrences",
			today.StepsCompleted, today.StepsTotal, today.IsCompleted)
	}
}

// Editing the template must not rewrite what a finished occurrence says
// happened: the snapshot on habit_step_completions is the historical record.
func TestHabitSteps_CompletedOccurrenceKeepsHistoricalSnapshot(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "Original wording")
	yesterday := time.Now().AddDate(0, 0, -1)
	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, yesterday, string(types.StepStatusCompleted)); err != nil {
		t.Fatalf("complete yesterday: %v", err)
	}

	newDesc := "Completely rewritten"
	if _, err := database.UpdateHabitStep(c, habitID, s1.HabitStepID, types.UpdateHabitStepRequest{Description: &newDesc}); err != nil {
		t.Fatalf("UpdateHabitStep: %v", err)
	}

	prev, err := database.GetOccurrenceSteps(c, habitID, yesterday)
	if err != nil {
		t.Fatalf("read yesterday: %v", err)
	}
	if prev.Steps[0].Description != "Original wording" {
		t.Errorf("history rewritten: got %q, want %q", prev.Steps[0].Description, "Original wording")
	}
	if !prev.IsCompleted {
		t.Error("previously completed occurrence must stay completed through a template edit")
	}

	// Future/untouched occurrences do follow the template.
	today, _ := database.GetOccurrenceSteps(c, habitID, time.Now())
	if today.Steps[0].Description != newDesc {
		t.Errorf("untouched occurrence did not pick up the edit: got %q, want %q", today.Steps[0].Description, newDesc)
	}
}

// Deleting a template step keeps the historical record of it (habit_step_id
// goes NULL, snapshot survives) rather than erasing a completed occurrence.
func TestHabitSteps_DeletingTemplateStepPreservesHistory(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "Historical step")
	yesterday := time.Now().AddDate(0, 0, -1)
	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, yesterday, string(types.StepStatusCompleted)); err != nil {
		t.Fatalf("complete yesterday: %v", err)
	}

	if err := database.DeleteHabitStep(c, habitID, s1.HabitStepID); err != nil {
		t.Fatalf("DeleteHabitStep: %v", err)
	}

	prev, err := database.GetOccurrenceSteps(c, habitID, yesterday)
	if err != nil {
		t.Fatalf("read yesterday: %v", err)
	}
	if len(prev.Steps) != 1 || prev.Steps[0].Description != "Historical step" {
		t.Fatalf("historical occurrence lost its step: %+v", prev.Steps)
	}
	if !prev.Steps[0].Completed {
		t.Error("historical completion state lost")
	}
}

// Removing every step returns the item to plain single-action completion.
func TestHabitSteps_DeletingAllStepsRestoresSingleActionCompletion(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusIncomplete)); err != nil {
		t.Fatalf("touch occurrence: %v", err)
	}
	if err := database.DeleteHabitStep(c, habitID, s1.HabitStepID); err != nil {
		t.Fatalf("DeleteHabitStep: %v", err)
	}

	occ, err := database.GetOccurrenceSteps(c, habitID, time.Now())
	if err != nil {
		t.Fatalf("GetOccurrenceSteps: %v", err)
	}
	if occ.StepsTotal != 0 {
		t.Fatalf("StepsTotal=%d after deleting all steps, want 0", occ.StepsTotal)
	}

	if _, err := database.CompleteHabit(c, habitID, time.Now(), ""); err != nil {
		t.Fatalf("CompleteHabit after step removal: %v", err)
	}
	if got := completionCount(t, habitID, time.Now()); got != 1 {
		t.Fatalf("completions=%d, want 1", got)
	}
}

// --- Template edits reaching the active occurrence ---------------------------

// A step added mid-occurrence joins the occurrence in progress (every other
// habit field edit takes effect immediately) and reopens it if it had been
// completed by the previously-final step... provided it is not already a
// finished, historical occurrence.
func TestHabitSteps_NewStepJoinsActiveIncompleteOccurrence(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	s1 := addStep(t, c, habitID, "one")
	if _, err := database.SetHabitStepState(c, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusIncomplete)); err != nil {
		t.Fatalf("materialise: %v", err)
	}

	addStep(t, c, habitID, "two")

	occ, err := database.GetOccurrenceSteps(c, habitID, time.Now())
	if err != nil {
		t.Fatalf("GetOccurrenceSteps: %v", err)
	}
	if occ.StepsTotal != 2 {
		t.Fatalf("StepsTotal=%d, want 2 -- new step did not reach the active occurrence", occ.StepsTotal)
	}
}

// --- Authorization -----------------------------------------------------------

// Knowing another user's habit UUID must not be enough to read or change it.
func TestHabitSteps_OtherUserCannotReadOrModify(t *testing.T) {
	owner := createUser(t, "UTC")
	intruder := createUser(t, "UTC")
	habitID := newHabit(t, owner)

	ownerCtx := stepCtx(owner)
	s1 := addStep(t, ownerCtx, habitID, "private step")

	intruderCtx := stepCtx(intruder)

	if _, err := database.GetOccurrenceSteps(intruderCtx, habitID, time.Now()); err != types.ErrHabitNotFound {
		t.Errorf("GetOccurrenceSteps as intruder: err=%v, want ErrHabitNotFound", err)
	}
	if _, err := database.GetHabitSteps(intruderCtx, habitID); err != types.ErrHabitNotFound {
		t.Errorf("GetHabitSteps as intruder: err=%v, want ErrHabitNotFound", err)
	}
	if _, err := database.SetHabitStepState(intruderCtx, habitID, s1.HabitStepID, time.Now(), string(types.StepStatusCompleted)); err != types.ErrHabitNotFound {
		t.Errorf("SetHabitStepState as intruder: err=%v, want ErrHabitNotFound", err)
	}
	if _, err := database.CreateHabitStep(intruderCtx, habitID, types.CreateHabitStepRequest{Description: "injected"}); err != types.ErrHabitNotFound {
		t.Errorf("CreateHabitStep as intruder: err=%v, want ErrHabitNotFound", err)
	}
	if err := database.DeleteHabitStep(intruderCtx, habitID, s1.HabitStepID); err != types.ErrHabitNotFound {
		t.Errorf("DeleteHabitStep as intruder: err=%v, want ErrHabitNotFound", err)
	}

	// And nothing actually changed.
	occ, err := database.GetOccurrenceSteps(ownerCtx, habitID, time.Now())
	if err != nil {
		t.Fatalf("owner read: %v", err)
	}
	if occ.StepsTotal != 1 || occ.StepsCompleted != 0 {
		t.Fatalf("owner sees %d/%d, want 0/1 -- intruder mutated state", occ.StepsCompleted, occ.StepsTotal)
	}
}

// CompleteHabit previously trusted the habit_id alone; completing another
// user's habit must now be rejected.
func TestHabitSteps_OtherUserCannotCompleteHabit(t *testing.T) {
	owner := createUser(t, "UTC")
	intruder := createUser(t, "UTC")
	habitID := newHabit(t, owner)

	if _, err := database.CompleteHabit(stepCtx(intruder), habitID, time.Now(), ""); err != types.ErrHabitNotFound {
		t.Errorf("CompleteHabit as intruder: err=%v, want ErrHabitNotFound", err)
	}
	if got := completionCount(t, habitID, time.Now()); got != 0 {
		t.Fatalf("completions=%d, want 0 -- intruder wrote a completion", got)
	}
}

// --- Direct completion path --------------------------------------------------

// Completing a stepped item through the plain endpoint brings its checklist
// along, so the occurrence never shows "done" over unchecked steps. Roadmap
// does the same via CompleteAllSteps.
func TestHabitSteps_DirectCompleteMarksStepsComplete(t *testing.T) {
	uid := createUser(t, "UTC")
	habitID := newHabit(t, uid)
	c := stepCtx(uid)

	addStep(t, c, habitID, "one")
	addStep(t, c, habitID, "two")

	if _, err := database.CompleteHabit(c, habitID, time.Now(), ""); err != nil {
		t.Fatalf("CompleteHabit: %v", err)
	}

	occ, err := database.GetOccurrenceSteps(c, habitID, time.Now())
	if err != nil {
		t.Fatalf("GetOccurrenceSteps: %v", err)
	}
	if occ.StepsCompleted != 2 || !occ.IsCompleted {
		t.Fatalf("got %d/%d completed=%v, want 2/2 complete", occ.StepsCompleted, occ.StepsTotal, occ.IsCompleted)
	}

	if err := database.UncompleteHabit(c, habitID, time.Now()); err != nil {
		t.Fatalf("UncompleteHabit: %v", err)
	}
	occ, _ = database.GetOccurrenceSteps(c, habitID, time.Now())
	if occ.StepsCompleted != 0 || occ.IsCompleted {
		t.Fatalf("after uncomplete got %d/%d completed=%v, want 0/2 incomplete", occ.StepsCompleted, occ.StepsTotal, occ.IsCompleted)
	}
}
