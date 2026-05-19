package testutil

import (
	"time"

	"dr/server/types"

	"github.com/google/uuid"
)

// RoadmapOpt mutates a Roadmap fixture. Construct via NewRoadmap(t, opts...).
type RoadmapOpt func(*types.Roadmap)

// MilestoneOpt mutates a Milestone fixture.
type MilestoneOpt func(*types.Milestone)

// TaskOpt mutates a Task fixture.
type TaskOpt func(*types.Task)

// NewRoadmap returns a Roadmap with sensible defaults: a generated UUID, the
// given owner, no start date (not yet started), and no children. Pass options
// to override fields or attach milestones/phases.
func NewRoadmap(userID string, opts ...RoadmapOpt) types.Roadmap {
	r := types.Roadmap{
		RoadmapID:        uuid.NewString(),
		UserID:           userID,
		Goal:             "test goal",
		Description:      "test roadmap",
		Days:             30,
		Type:             string(types.RoadmapTypeCustomMade),
		GenerationStatus: string(types.GenerationStatusCompleted),
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

// WithStartDate marks the roadmap as started on the given date.
func WithStartDate(d time.Time) RoadmapOpt {
	return func(r *types.Roadmap) { r.StartDate = &d }
}

// WithArchived flags the roadmap as archived.
func WithArchived() RoadmapOpt {
	return func(r *types.Roadmap) { r.Archived = true }
}

// WithPausedAt flags the roadmap as paused at the given time.
func WithPausedAt(t time.Time) RoadmapOpt {
	return func(r *types.Roadmap) { r.PausedAt = &t }
}

// WithMilestones attaches the given milestones to the roadmap (as top-level,
// not nested in a phase).
func WithMilestones(ms ...types.Milestone) RoadmapOpt {
	return func(r *types.Roadmap) { r.Milestones = append(r.Milestones, ms...) }
}

// NewMilestone returns a Milestone with sensible defaults: a generated UUID
// and an empty Tasks slice. The RoadmapID/UserID/PhaseID can be wired by the
// caller via options.
func NewMilestone(opts ...MilestoneOpt) types.Milestone {
	m := types.Milestone{
		MilestoneID: uuid.NewString(),
		SubGoal:     "milestone goal",
	}
	for _, o := range opts {
		o(&m)
	}
	return m
}

// WithMilestoneRoadmap sets the RoadmapID and UserID to match a roadmap.
func WithMilestoneRoadmap(r *types.Roadmap) MilestoneOpt {
	return func(m *types.Milestone) {
		m.RoadmapID = r.RoadmapID
		m.UserID = r.UserID
	}
}

// WithTasks attaches the given tasks to the milestone.
func WithTasks(ts ...types.Task) MilestoneOpt {
	return func(m *types.Milestone) { m.Tasks = append(m.Tasks, ts...) }
}

// WithMilestoneReached marks the milestone as reached (the lifecycle's
// completion flag, independent of whether each task is completed).
func WithMilestoneReached() MilestoneOpt {
	return func(m *types.Milestone) { m.Reached = true }
}

// NewTask returns a Task with sensible defaults: a generated UUID, not
// completed, and no date. Add a date via WithTaskDate; mark completed via
// WithTaskCompleted.
func NewTask(opts ...TaskOpt) types.Task {
	t := types.Task{
		TaskID: uuid.NewString(),
		Title:  "test task",
	}
	for _, o := range opts {
		o(&t)
	}
	return t
}

// WithTaskDate sets the task's scheduled date.
func WithTaskDate(d time.Time) TaskOpt {
	return func(t *types.Task) { t.Date = &d }
}

// WithTaskCompleted marks the task as completed.
func WithTaskCompleted() TaskOpt {
	return func(t *types.Task) { t.Completed = true }
}

// WithTaskMilestone wires the task to a milestone (and the milestone's parent
// roadmap/user) so foreign-key references are consistent.
func WithTaskMilestone(m *types.Milestone) TaskOpt {
	return func(t *types.Task) {
		t.MilestoneID = m.MilestoneID
		t.RoadmapID = m.RoadmapID
		t.UserID = m.UserID
	}
}
