package database

import (
	"sort"

	"dr/server/types"
)

// taskPath is a task's 1-based position within its roadmap hierarchy.
type taskPath struct {
	PhaseNumber     int
	MilestoneNumber int
	TaskNumber      int
}

// pathMilestone / pathPhase / pathTask are the minimal row shapes needed to
// rank the hierarchy. Kept separate from the full domain types so the ranking
// logic is a pure function over plain data.
type pathMilestone struct {
	MilestoneID string
	RoadmapID   string
	PhaseID     *string
	Day         int
	OrderIndex  int
}

type pathPhase struct {
	PhaseID    string
	RoadmapID  string
	OrderIndex int
}

type pathTask struct {
	TaskID      string
	MilestoneID string
	Day         int
}

// computeTaskPaths ranks the hierarchy and returns each task's path.
// Ordering mirrors the canonical display/status order used elsewhere
// (RecomputeAndSaveStatuses): phases by order_index, milestones by day
// (order_index then ID as tie-breaks), tasks within a milestone by day.
// Milestone numbers are global within the roadmap, matching the tactical
// milestone list the user sees.
func computeTaskPaths(milestones []pathMilestone, phases []pathPhase, tasks []pathTask) map[string]taskPath {
	phaseNumber := make(map[string]int, len(phases))
	sort.SliceStable(phases, func(i, j int) bool {
		if phases[i].RoadmapID != phases[j].RoadmapID {
			return phases[i].RoadmapID < phases[j].RoadmapID
		}
		if phases[i].OrderIndex != phases[j].OrderIndex {
			return phases[i].OrderIndex < phases[j].OrderIndex
		}
		return phases[i].PhaseID < phases[j].PhaseID
	})
	rank := 0
	for i, p := range phases {
		if i == 0 || p.RoadmapID != phases[i-1].RoadmapID {
			rank = 0
		}
		rank++
		phaseNumber[p.PhaseID] = rank
	}

	milestoneNumber := make(map[string]int, len(milestones))
	milestonePhase := make(map[string]*string, len(milestones))
	sort.SliceStable(milestones, func(i, j int) bool {
		if milestones[i].RoadmapID != milestones[j].RoadmapID {
			return milestones[i].RoadmapID < milestones[j].RoadmapID
		}
		if milestones[i].Day != milestones[j].Day {
			return milestones[i].Day < milestones[j].Day
		}
		if milestones[i].OrderIndex != milestones[j].OrderIndex {
			return milestones[i].OrderIndex < milestones[j].OrderIndex
		}
		return milestones[i].MilestoneID < milestones[j].MilestoneID
	})
	rank = 0
	for i, m := range milestones {
		if i == 0 || m.RoadmapID != milestones[i-1].RoadmapID {
			rank = 0
		}
		rank++
		milestoneNumber[m.MilestoneID] = rank
		milestonePhase[m.MilestoneID] = m.PhaseID
	}

	taskNumber := make(map[string]int, len(tasks))
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].MilestoneID != tasks[j].MilestoneID {
			return tasks[i].MilestoneID < tasks[j].MilestoneID
		}
		if tasks[i].Day != tasks[j].Day {
			return tasks[i].Day < tasks[j].Day
		}
		return tasks[i].TaskID < tasks[j].TaskID
	})
	rank = 0
	for i, t := range tasks {
		if i == 0 || t.MilestoneID != tasks[i-1].MilestoneID {
			rank = 0
		}
		rank++
		taskNumber[t.TaskID] = rank
	}

	paths := make(map[string]taskPath, len(tasks))
	for _, t := range tasks {
		mNum, ok := milestoneNumber[t.MilestoneID]
		if !ok {
			continue
		}
		path := taskPath{MilestoneNumber: mNum, TaskNumber: taskNumber[t.TaskID]}
		if pid := milestonePhase[t.MilestoneID]; pid != nil && *pid != "" {
			path.PhaseNumber = phaseNumber[*pid]
		}
		paths[t.TaskID] = path
	}
	return paths
}

// AnnotateTaskPaths fills PhaseNumber/MilestoneNumber/TaskNumber on the given
// tasks (drserver#47: the Today-tab badge). It loads the sibling milestones,
// phases, and tasks for the roadmaps involved and ranks them in memory —
// roadmaps are small, so three narrow queries beat a window-function join.
// Tasks without a milestone are left unannotated. Errors are swallowed
// (annotation is cosmetic; the task list itself must still be served).
func AnnotateTaskPaths(tasks []types.Task) {
	if len(tasks) == 0 {
		return
	}

	roadmapIDSet := make(map[string]struct{})
	for _, t := range tasks {
		if t.RoadmapID != "" && t.MilestoneID != "" {
			roadmapIDSet[t.RoadmapID] = struct{}{}
		}
	}
	if len(roadmapIDSet) == 0 {
		return
	}
	roadmapIDs := make([]string, 0, len(roadmapIDSet))
	for id := range roadmapIDSet {
		roadmapIDs = append(roadmapIDs, id)
	}
	sort.Strings(roadmapIDs)

	var milestones []pathMilestone
	if err := db.Model(&types.Milestone{}).
		Select("milestone_id, roadmap_id, phase_id, day, order_index").
		Where("roadmap_id IN ?", roadmapIDs).
		Scan(&milestones).Error; err != nil {
		return
	}

	var phases []pathPhase
	if err := db.Model(&types.Phase{}).
		Select("phase_id, roadmap_id, order_index").
		Where("roadmap_id IN ?", roadmapIDs).
		Scan(&phases).Error; err != nil {
		return
	}

	var siblingTasks []pathTask
	if err := db.Model(&types.Task{}).
		Select("task_id, milestone_id, day").
		Where("roadmap_id IN ?", roadmapIDs).
		Scan(&siblingTasks).Error; err != nil {
		return
	}

	paths := computeTaskPaths(milestones, phases, siblingTasks)
	for i := range tasks {
		if p, ok := paths[tasks[i].TaskID]; ok {
			tasks[i].PhaseNumber = p.PhaseNumber
			tasks[i].MilestoneNumber = p.MilestoneNumber
			tasks[i].TaskNumber = p.TaskNumber
		}
	}
}
