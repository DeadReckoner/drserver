package database

import (
	"testing"

	"dr/server/types"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestComputeTaskPaths_RanksHierarchy(t *testing.T) {
	// Roadmap R: Phase A (order 0) ← M1 (day 1), Phase B (order 1) ← M2 (day 10).
	// M1 has tasks day 1 and day 3; M2 has one task day 10.
	milestones := []pathMilestone{
		{MilestoneID: "m2", RoadmapID: "r", PhaseID: strPtr("pb"), Day: 10},
		{MilestoneID: "m1", RoadmapID: "r", PhaseID: strPtr("pa"), Day: 1},
	}
	phases := []pathPhase{
		{PhaseID: "pb", RoadmapID: "r", OrderIndex: 1},
		{PhaseID: "pa", RoadmapID: "r", OrderIndex: 0},
	}
	tasks := []pathTask{
		{TaskID: "t3", MilestoneID: "m2", Day: 10},
		{TaskID: "t2", MilestoneID: "m1", Day: 3},
		{TaskID: "t1", MilestoneID: "m1", Day: 1},
	}

	paths := computeTaskPaths(milestones, phases, tasks)

	assert.Equal(t, taskPath{PhaseNumber: 1, MilestoneNumber: 1, TaskNumber: 1}, paths["t1"])
	assert.Equal(t, taskPath{PhaseNumber: 1, MilestoneNumber: 1, TaskNumber: 2}, paths["t2"])
	assert.Equal(t, taskPath{PhaseNumber: 2, MilestoneNumber: 2, TaskNumber: 1}, paths["t3"])
}

func TestComputeTaskPaths_NoPhase(t *testing.T) {
	// Tactical-only roadmap: milestones without phases still get numbered,
	// phase number stays 0 (omitted from JSON).
	milestones := []pathMilestone{
		{MilestoneID: "m1", RoadmapID: "r", Day: 1},
		{MilestoneID: "m2", RoadmapID: "r", Day: 5},
	}
	tasks := []pathTask{
		{TaskID: "t1", MilestoneID: "m2", Day: 5},
	}

	paths := computeTaskPaths(milestones, nil, tasks)

	assert.Equal(t, taskPath{PhaseNumber: 0, MilestoneNumber: 2, TaskNumber: 1}, paths["t1"])
}

func TestComputeTaskPaths_NumbersArePerRoadmap(t *testing.T) {
	// Two roadmaps must each start numbering at 1.
	milestones := []pathMilestone{
		{MilestoneID: "a-m1", RoadmapID: "a", Day: 1},
		{MilestoneID: "b-m1", RoadmapID: "b", Day: 1},
	}
	tasks := []pathTask{
		{TaskID: "a-t1", MilestoneID: "a-m1", Day: 1},
		{TaskID: "b-t1", MilestoneID: "b-m1", Day: 1},
	}

	paths := computeTaskPaths(milestones, nil, tasks)

	assert.Equal(t, 1, paths["a-t1"].MilestoneNumber)
	assert.Equal(t, 1, paths["b-t1"].MilestoneNumber)
}

func TestComputeTaskPaths_SameDayTieBreaksByID(t *testing.T) {
	milestones := []pathMilestone{{MilestoneID: "m1", RoadmapID: "r", Day: 1}}
	tasks := []pathTask{
		{TaskID: "t-b", MilestoneID: "m1", Day: 2},
		{TaskID: "t-a", MilestoneID: "m1", Day: 2},
	}

	paths := computeTaskPaths(milestones, nil, tasks)

	assert.Equal(t, 1, paths["t-a"].TaskNumber)
	assert.Equal(t, 2, paths["t-b"].TaskNumber)
}

func TestAnnotateTaskPaths_FillsFieldsFromDB(t *testing.T) {
	mock := withMockDB(t)

	tasks := []types.Task{
		{TaskID: "t1", RoadmapID: "r", MilestoneID: "m1"},
	}

	mock.ExpectQuery(`SELECT milestone_id, roadmap_id, phase_id, day, order_index FROM "milestones" WHERE roadmap_id IN \(\$1\)`).
		WithArgs("r").
		WillReturnRows(sqlmock.NewRows([]string{"milestone_id", "roadmap_id", "phase_id", "day", "order_index"}).
			AddRow("m1", "r", "p1", 1, 0))

	mock.ExpectQuery(`SELECT phase_id, roadmap_id, order_index FROM "phases" WHERE roadmap_id IN \(\$1\)`).
		WithArgs("r").
		WillReturnRows(sqlmock.NewRows([]string{"phase_id", "roadmap_id", "order_index"}).
			AddRow("p1", "r", 0))

	mock.ExpectQuery(`SELECT task_id, milestone_id, day FROM "tasks" WHERE roadmap_id IN \(\$1\)`).
		WithArgs("r").
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "milestone_id", "day"}).
			AddRow("t0", "m1", 1).
			AddRow("t1", "m1", 2))

	AnnotateTaskPaths(tasks)

	require.Equal(t, 1, tasks[0].PhaseNumber)
	require.Equal(t, 1, tasks[0].MilestoneNumber)
	assert.Equal(t, 2, tasks[0].TaskNumber, "t1 is the second task in m1 by day")
}

func TestAnnotateTaskPaths_NoMilestoneTasksSkipped(t *testing.T) {
	// Tasks with no milestone should not trigger any queries.
	withMockDB(t)

	tasks := []types.Task{{TaskID: "t1", RoadmapID: "r"}}
	AnnotateTaskPaths(tasks)

	assert.Zero(t, tasks[0].MilestoneNumber)
	assert.Zero(t, tasks[0].TaskNumber)
}
