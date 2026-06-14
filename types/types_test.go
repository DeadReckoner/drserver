package types_test

import (
	"testing"
	"time"

	"dr/server/internal/testutil"
	"dr/server/types"
)

// today is the frozen "now" used by every status test in this file. Picked
// deliberately to land mid-week with no DST or month-boundary weirdness so
// arithmetic in the test cases reads cleanly.
var today = time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)

func freeze(t *testing.T) { testutil.FreezeTime(t, today) }

func dayOffset(n int) *time.Time {
	d := today.AddDate(0, 0, n)
	return &d
}

// --- 1. Roadmap not started: nothing transitions to in_progress -------------

func TestCompute_NotStarted_AllTasksStayNotStarted(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1", testutil.WithMilestones(
		testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(-5))), // would be overdue if started
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(1))),
		)),
	))

	r.ComputeAllStatuses()

	for i, task := range r.Milestones[0].Tasks {
		if task.Status != string(types.StatusNotStarted) {
			t.Errorf("task %d: got %q, want %q (roadmap not started)", i, task.Status, types.StatusNotStarted)
		}
	}
	if got := r.Milestones[0].Status; got != string(types.StatusNotStarted) {
		t.Errorf("milestone: got %q, want %q", got, types.StatusNotStarted)
	}
}

// --- 2. Single-task transitions ---------------------------------------------

func TestCompute_StartedSingleTask_NotCompletedFutureDate_InProgress(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(1))),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("task: got %q, want %q", got, types.StatusInProgress)
	}
	if got := r.Milestones[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("milestone: got %q, want %q", got, types.StatusInProgress)
	}
}

func TestCompute_StartedSingleTask_PastDate_Overdue(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(-1))),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("task: got %q, want %q", got, types.StatusOverdue)
	}
	if got := r.Milestones[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("milestone: got %q, want %q", got, types.StatusOverdue)
	}
}

func TestCompute_StartedSingleTask_Completed(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(-1)), testutil.WithTaskCompleted()),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("task: got %q, want %q", got, types.StatusCompleted)
	}
	if got := r.Milestones[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("milestone (all tasks done): got %q, want %q", got, types.StatusCompleted)
	}
}

func TestCompute_StartedTask_NilDate_InProgressNotOverdue(t *testing.T) {
	freeze(t)

	// Task with no date should be in_progress (current task) but not overdue.
	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("task: got %q, want %q", got, types.StatusInProgress)
	}
}

// --- 3. The "only one in_progress task per roadmap" invariant ---------------

func TestCompute_AllPastDueTasksAreOverdue(t *testing.T) {
	// Overdue is an independent property: every incomplete past-due task is
	// overdue, not just the first/current one. (Regression for the bug where
	// only the single current task could ever show overdue, so users behind
	// on several tasks saw at most one flagged.)
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(-3))), // current + overdue
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(-2))), // also past due → overdue
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(2))),  // future → not_started
		))),
	)
	r.ComputeAllStatuses()

	ts := r.Milestones[0].Tasks
	if ts[0].Status != string(types.StatusOverdue) {
		t.Errorf("task[0] (current + past date): got %q, want %q", ts[0].Status, types.StatusOverdue)
	}
	if ts[1].Status != string(types.StatusOverdue) {
		t.Errorf("task[1] (later, still past due): got %q, want %q", ts[1].Status, types.StatusOverdue)
	}
	if ts[2].Status != string(types.StatusNotStarted) {
		t.Errorf("task[2] (future): got %q, want %q", ts[2].Status, types.StatusNotStarted)
	}
}

func TestCompute_NoInProgressWhileBehind(t *testing.T) {
	// The one-current-task model: when the current (first incomplete) task is
	// itself overdue, no separate future task is promoted to in_progress.
	// At most one in_progress across the roadmap, and it never coexists with
	// the user being behind.
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(-2))), // overdue (current)
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(1))),  // future
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(3))),  // future
		))),
	)
	r.ComputeAllStatuses()

	ts := r.Milestones[0].Tasks
	if ts[0].Status != string(types.StatusOverdue) {
		t.Errorf("task[0]: got %q, want %q", ts[0].Status, types.StatusOverdue)
	}
	for i := 1; i < len(ts); i++ {
		if ts[i].Status == string(types.StatusInProgress) {
			t.Errorf("task[%d]: got in_progress, want not_started (no in_progress while a current overdue task exists)", i)
		}
		if ts[i].Status != string(types.StatusNotStarted) {
			t.Errorf("task[%d]: got %q, want %q", i, ts[i].Status, types.StatusNotStarted)
		}
	}
}

func TestCompute_OnTimeCurrentTaskIsInProgress_LaterTasksNotStarted(t *testing.T) {
	// When the current task is on time, it is in_progress and later tasks are
	// not_started — the original happy path, unchanged by the overdue fix.
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(1))), // current, on time
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(2))),
			testutil.NewTask(testutil.WithTaskDate(*dayOffset(3))),
		))),
	)
	r.ComputeAllStatuses()

	ts := r.Milestones[0].Tasks
	if ts[0].Status != string(types.StatusInProgress) {
		t.Errorf("task[0] (on-time current): got %q, want %q", ts[0].Status, types.StatusInProgress)
	}
	for i := 1; i < len(ts); i++ {
		if ts[i].Status != string(types.StatusNotStarted) {
			t.Errorf("task[%d]: got %q, want %q", i, ts[i].Status, types.StatusNotStarted)
		}
	}
}

func TestCompute_OverdueSpansMilestones(t *testing.T) {
	// Past-due incomplete tasks in a LATER milestone are also overdue — the
	// fix is not limited to the current milestone. Mirrors the real-world
	// case of falling behind across milestone boundaries.
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-10)),
		testutil.WithMilestones(
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(-5))), // overdue (current)
			)),
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(-3))), // overdue, later milestone
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(4))),  // future
			)),
		),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("m0.t0: got %q, want %q", got, types.StatusOverdue)
	}
	if got := r.Milestones[1].Tasks[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("m1.t0 (past due in later milestone): got %q, want %q", got, types.StatusOverdue)
	}
	if got := r.Milestones[1].Tasks[1].Status; got != string(types.StatusNotStarted) {
		t.Errorf("m1.t1 (future): got %q, want %q", got, types.StatusNotStarted)
	}
	// Both milestones roll up to overdue.
	if got := r.Milestones[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("m0 status: got %q, want %q", got, types.StatusOverdue)
	}
	if got := r.Milestones[1].Status; got != string(types.StatusOverdue) {
		t.Errorf("m1 status: got %q, want %q", got, types.StatusOverdue)
	}
}

func TestCompute_OnlyOneInProgressAcrossMultipleMilestones(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(-3)), testutil.WithTaskCompleted()),
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(-1))), // first incomplete: gets in_progress (overdue)
			)),
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(1))),
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(2))),
			)),
		),
	)
	r.ComputeAllStatuses()

	// Milestone 0: 1 completed + 1 overdue (the current task).
	if got := r.Milestones[0].Tasks[1].Status; got != string(types.StatusOverdue) {
		t.Errorf("m0.t1: got %q, want %q", got, types.StatusOverdue)
	}
	// Milestone 1: every task must be not_started, even though dates are in
	// the future (or could be past in other tests). The invariant: only one
	// "current" task per roadmap.
	for i, task := range r.Milestones[1].Tasks {
		if task.Status != string(types.StatusNotStarted) {
			t.Errorf("m1.t%d: got %q, want %q (subsequent milestones cannot host the current task)", i, task.Status, types.StatusNotStarted)
		}
	}
}

// --- 4. Milestone status derivation -----------------------------------------

func TestCompute_MilestoneStatus_AllTasksCompleted(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskCompleted()),
			testutil.NewTask(testutil.WithTaskCompleted()),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("milestone: got %q, want %q", got, types.StatusCompleted)
	}
}

func TestCompute_MilestoneStatus_OverdueBeatsInProgress(t *testing.T) {
	// Per computeMilestoneStatus, overdue takes precedence over in_progress
	// when both are present. With the single-current-task invariant, this
	// shouldn't occur in practice within one milestone (the current task is
	// either overdue OR in_progress, not both), but the cascade can produce
	// it at the phase level when one milestone is overdue and another is
	// in_progress. We test the precedence directly.
	freeze(t)

	// Two milestones: one with the current overdue task, one not yet
	// reachable. Roll up to a phase.
	overdueMilestone := testutil.NewMilestone(testutil.WithTasks(
		testutil.NewTask(testutil.WithTaskDate(*dayOffset(-2))),
	))
	laterMilestone := testutil.NewMilestone(testutil.WithTasks(
		testutil.NewTask(testutil.WithTaskDate(*dayOffset(5))),
	))

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithPhases(testutil.NewPhase(testutil.WithPhaseMilestones(overdueMilestone, laterMilestone))),
	)
	r.ComputeAllStatuses()

	if got := r.Phases[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("phase: got %q, want %q (overdue child wins)", got, types.StatusOverdue)
	}
}

func TestCompute_MilestoneReached_AlwaysCompleted(t *testing.T) {
	freeze(t)

	// Even with incomplete child tasks, a Reached=true milestone is treated
	// as completed.
	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone(
			testutil.WithMilestoneReached(),
			testutil.WithTasks(testutil.NewTask(testutil.WithTaskDate(*dayOffset(1)))),
		)),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("Reached milestone: got %q, want %q", got, types.StatusCompleted)
	}
}

// --- 5. Empty-milestone behavior --------------------------------------------

func TestCompute_EmptyCurrentMilestone_RoadmapStarted_InProgress(t *testing.T) {
	// Per the comment in types.go: "If this is the current milestone and
	// roadmap has started, mark as in_progress so the UI can show the
	// generate tasks button."
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithMilestones(testutil.NewMilestone()), // no tasks
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("empty current milestone: got %q, want %q", got, types.StatusInProgress)
	}
}

func TestCompute_EmptyMilestone_RoadmapNotStarted_NotStarted(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithMilestones(testutil.NewMilestone()), // no tasks, no start date
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Status; got != string(types.StatusNotStarted) {
		t.Errorf("empty milestone, roadmap unstarted: got %q, want %q", got, types.StatusNotStarted)
	}
}

// --- 6. Regression: next milestone moves to in_progress (commit 61f52f6) ----

func TestCompute_NextMilestoneFlipsToInProgress_AfterPreviousCompletes(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithMilestones(
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskCompleted()),
				testutil.NewTask(testutil.WithTaskCompleted()),
			)),
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(1))),
				testutil.NewTask(testutil.WithTaskDate(*dayOffset(2))),
			)),
		),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Status; got != string(types.StatusCompleted) {
		t.Fatalf("m0 (all tasks done): got %q, want %q", got, types.StatusCompleted)
	}
	if got := r.Milestones[1].Tasks[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("m1.t0 (first task of next milestone): got %q, want %q — regression: next milestone should pick up in_progress after previous completes", got, types.StatusInProgress)
	}
	if got := r.Milestones[1].Status; got != string(types.StatusInProgress) {
		t.Errorf("m1 (next milestone): got %q, want %q", got, types.StatusInProgress)
	}
}

func TestCompute_NextMilestoneEmpty_FlipsToInProgress_AfterPreviousCompletes(t *testing.T) {
	// Same regression but the next milestone has no tasks yet — should still
	// flip to in_progress (UI hint: "generate tasks").
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithMilestones(
			testutil.NewMilestone(testutil.WithTasks(
				testutil.NewTask(testutil.WithTaskCompleted()),
			)),
			testutil.NewMilestone(),
		),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[1].Status; got != string(types.StatusInProgress) {
		t.Errorf("empty next milestone: got %q, want %q", got, types.StatusInProgress)
	}
}

// --- 7. Phase cascade -------------------------------------------------------

func TestCompute_PhaseStatus_DerivedFromMilestones(t *testing.T) {
	freeze(t)

	// Phase contains two milestones, both completed → phase completed.
	completedM := testutil.NewMilestone(testutil.WithTasks(
		testutil.NewTask(testutil.WithTaskCompleted()),
	))
	completedM2 := testutil.NewMilestone(testutil.WithTasks(
		testutil.NewTask(testutil.WithTaskCompleted()),
	))

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithPhases(testutil.NewPhase(testutil.WithPhaseMilestones(completedM, completedM2))),
	)
	r.ComputeAllStatuses()

	if got := r.Phases[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("phase (all milestones done): got %q, want %q", got, types.StatusCompleted)
	}
}

func TestCompute_EmptyPhase_NotStarted(t *testing.T) {
	freeze(t)

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithPhases(testutil.NewPhase()), // no milestones
	)
	r.ComputeAllStatuses()

	if got := r.Phases[0].Status; got != string(types.StatusNotStarted) {
		t.Errorf("empty phase: got %q, want %q", got, types.StatusNotStarted)
	}
}

// --- 8. Step 5: top-level milestones sync from phase counterparts -----------

func TestCompute_PhaseNestedMilestones_SyncToTopLevel(t *testing.T) {
	// Models the real shape: the same milestone appears in r.Phases[*].Milestones
	// AND in r.Milestones (with PhaseID set). After Compute, the top-level
	// copy's status (and its tasks' statuses) must match the phase-nested copy.
	freeze(t)

	completedTask := testutil.NewTask(testutil.WithTaskCompleted())
	milestone := testutil.NewMilestone(testutil.WithTasks(completedTask))

	phase := testutil.NewPhase(testutil.WithPhaseMilestones(milestone))

	// Reconstruct a top-level milestone pointing at the same MilestoneID +
	// task IDs, with PhaseID set. PhaseID was stamped by WithPhaseMilestones.
	topLevelMilestone := types.Milestone{
		MilestoneID: phase.Milestones[0].MilestoneID,
		PhaseID:     phase.Milestones[0].PhaseID,
		Tasks: []types.Task{
			{TaskID: completedTask.TaskID, Completed: true},
		},
	}

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-5)),
		testutil.WithPhases(phase),
		testutil.WithMilestones(topLevelMilestone),
	)
	r.ComputeAllStatuses()

	if got := r.Phases[0].Milestones[0].Status; got != string(types.StatusCompleted) {
		t.Fatalf("phase-nested milestone: got %q, want %q", got, types.StatusCompleted)
	}
	// The top-level copy must reflect the same status (Step 5 sync).
	if got := r.Milestones[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("top-level milestone (should mirror phase-nested copy): got %q, want %q", got, types.StatusCompleted)
	}
	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("top-level task (should mirror phase-nested copy): got %q, want %q", got, types.StatusCompleted)
	}
}

func TestCompute_TopLevelMilestoneWithPhaseID_NotProcessedTwice(t *testing.T) {
	// The algorithm skips top-level milestones with PhaseID in steps 2 and 3
	// to avoid double-processing. We verify this by giving the phase-nested
	// copy a completed task and the top-level copy a NOT completed task with
	// the same TaskID: after Compute, the top-level task should reflect the
	// phase-nested copy's "completed" status via Step 5 sync, not its own
	// initial value.
	freeze(t)

	taskID := "shared-task-id"
	phaseNestedTask := types.Task{TaskID: taskID, Completed: true}
	topLevelTask := types.Task{TaskID: taskID, Completed: false}

	phaseMilestone := types.Milestone{MilestoneID: "shared-ms-id", Tasks: []types.Task{phaseNestedTask}}
	phaseID := "phase-1"
	phaseMilestone.PhaseID = &phaseID

	topLevelMilestone := types.Milestone{
		MilestoneID: "shared-ms-id",
		PhaseID:     &phaseID,
		Tasks:       []types.Task{topLevelTask},
	}

	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(*dayOffset(-1)),
		testutil.WithPhases(types.Phase{PhaseID: phaseID, Milestones: []types.Milestone{phaseMilestone}}),
		testutil.WithMilestones(topLevelMilestone),
	)
	r.ComputeAllStatuses()

	// The phase-nested copy drives the result (its task is Completed=true).
	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusCompleted) {
		t.Errorf("top-level task status after sync: got %q, want %q (phase-nested copy is authoritative)", got, types.StatusCompleted)
	}
}

// --- 9. Empty roadmap doesn't panic ----------------------------------------

func TestCompute_EmptyRoadmap_NoPanic(t *testing.T) {
	freeze(t)
	r := testutil.NewRoadmap("u1")
	r.ComputeAllStatuses() // must not panic
}

// --- 10. Midnight boundary --------------------------------------------------

func TestCompute_OverdueBoundary_TaskDueToday_NotOverdue(t *testing.T) {
	// A task whose date is today should NOT be overdue. isDateBeforeToday
	// strips time components and uses Before(), so equal dates fall through.
	testutil.FreezeTime(t, time.Date(2026, 5, 19, 23, 59, 59, 0, time.UTC))

	dueToday := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(dueToday)),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusInProgress) {
		t.Errorf("task due today at end-of-day: got %q, want %q (today is not 'before today')", got, types.StatusInProgress)
	}
}

func TestCompute_OverdueBoundary_TaskDueYesterday_Overdue(t *testing.T) {
	testutil.FreezeTime(t, time.Date(2026, 5, 19, 0, 0, 1, 0, time.UTC)) // one second past midnight

	dueYesterday := time.Date(2026, 5, 18, 23, 0, 0, 0, time.UTC)
	r := testutil.NewRoadmap("u1",
		testutil.WithStartDate(time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)),
		testutil.WithMilestones(testutil.NewMilestone(testutil.WithTasks(
			testutil.NewTask(testutil.WithTaskDate(dueYesterday)),
		))),
	)
	r.ComputeAllStatuses()

	if got := r.Milestones[0].Tasks[0].Status; got != string(types.StatusOverdue) {
		t.Errorf("task due yesterday at 23:00: got %q, want %q", got, types.StatusOverdue)
	}
}
