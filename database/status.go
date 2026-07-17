package database

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"dr/server/types"

	"gorm.io/gorm"
)

// bulkUpdateStatus updates the status column for many rows in a single query
// using UPDATE ... FROM (VALUES ...) syntax. If statuses is empty it's a no-op.
func bulkUpdateStatus(tx *gorm.DB, table, idCol string, statuses map[string]string) error {
	if len(statuses) == 0 {
		return nil
	}

	// Build: UPDATE <table> SET status = v.status
	//        FROM (VALUES ('id1','s1'),('id2','s2'),...) AS v(id, status)
	//        WHERE <table>.<idCol> = v.id
	vals := make([]string, 0, len(statuses))
	args := make([]interface{}, 0, len(statuses)*2)
	for id, status := range statuses {
		vals = append(vals, "(?, ?)")
		args = append(args, id, status)
	}

	sql := fmt.Sprintf(
		"UPDATE %s SET status = v.status FROM (VALUES %s) AS v(id, status) WHERE %s.%s = v.id",
		table, strings.Join(vals, ","), table, idCol,
	)
	return tx.Exec(sql, args...).Error
}

// resolveUserLocation loads a user's IANA timezone and returns the matching
// *time.Location, falling back to UTC when the user is missing, has no
// timezone set, or the stored value doesn't parse. Used to make the overdue
// boundary in ComputeAllStatusesInLocation match the user's local day, the
// same way the notification jobs compare against users.timezone.
func resolveUserLocation(userID string) *time.Location {
	if userID == "" {
		return time.UTC
	}
	var tz string
	if err := db.Model(&types.User{}).
		Where("user_id = ?", userID).
		Pluck("timezone", &tz).Error; err != nil {
		return time.UTC
	}
	return types.LocationOrUTC(tz)
}

// loadRoadmapHierarchy loads a roadmap and all its children using parallel
// queries (phases, milestones, tasks all fire concurrently), then assembles
// the GORM-style nested hierarchy in memory. This replaces 6 sequential
// Preload queries with 3 concurrent ones + 1 roadmap fetch.
func loadRoadmapHierarchy(roadmapID string) (types.Roadmap, error) {
	var roadmap types.Roadmap
	if err := db.Where("roadmap_id = ?", roadmapID).First(&roadmap).Error; err != nil {
		return roadmap, err
	}

	var (
		phases     []types.Phase
		milestones []types.Milestone
		tasks      []types.Task
		wg         sync.WaitGroup
		errPhases  error
		errMS      error
		errTasks   error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		errPhases = db.Where("roadmap_id = ?", roadmapID).Order("order_index ASC").Find(&phases).Error
	}()
	go func() {
		defer wg.Done()
		errMS = db.Where("roadmap_id = ?", roadmapID).Order("day ASC").Find(&milestones).Error
	}()
	go func() {
		defer wg.Done()
		errTasks = db.Where("roadmap_id = ?", roadmapID).Order("day ASC").Find(&tasks).Error
	}()
	wg.Wait()

	for _, e := range []error{errPhases, errMS, errTasks} {
		if e != nil {
			return roadmap, e
		}
	}

	// Index tasks by milestone_id
	tasksByMS := make(map[string][]types.Task, len(milestones))
	for _, t := range tasks {
		tasksByMS[t.MilestoneID] = append(tasksByMS[t.MilestoneID], t)
	}

	// Attach tasks to milestones, then milestones to phases / roadmap
	msByID := make(map[string]*types.Milestone, len(milestones))
	for i := range milestones {
		milestones[i].Tasks = tasksByMS[milestones[i].MilestoneID]
		msByID[milestones[i].MilestoneID] = &milestones[i]
	}

	msByPhase := make(map[string][]types.Milestone)
	var topLevelMS []types.Milestone
	for _, m := range milestones {
		if m.PhaseID != nil && *m.PhaseID != "" {
			msByPhase[*m.PhaseID] = append(msByPhase[*m.PhaseID], m)
		} else {
			topLevelMS = append(topLevelMS, m)
		}
	}

	for i := range phases {
		phases[i].Milestones = msByPhase[phases[i].PhaseID]
	}

	roadmap.Phases = phases
	roadmap.Milestones = topLevelMS
	return roadmap, nil
}

// RecomputeAndSaveStatuses loads a roadmap with all nested data, recomputes all statuses
// using Roadmap.ComputeAllStatuses(), and saves all statuses to the database.
// This is the single entry point for status recomputation.
func RecomputeAndSaveStatuses(roadmapID string) error {
	roadmap, err := loadRoadmapHierarchy(roadmapID)
	if err != nil {
		return err
	}

	roadmap.ComputeAllStatusesInLocation(resolveUserLocation(roadmap.UserID))

	return saveAllStatuses(&roadmap)
}

// CascadeCompletionAndRecompute handles the full cascade after a task's
// completion state may have changed: checks whether the task/milestone/roadmap
// should flip their completed/reached flags, then recomputes and persists all
// statuses. Loads the hierarchy once and writes everything in one transaction,
// replacing the previous pattern of separate shouldComplete* queries +
// individual updates + a second full load inside RecomputeAndSaveStatuses.
func CascadeCompletionAndRecompute(roadmapID string) error {
	roadmap, err := loadRoadmapHierarchy(roadmapID)
	if err != nil {
		return err
	}

	loc := resolveUserLocation(roadmap.UserID)
	roadmap.ComputeAllStatusesInLocation(loc)

	// Determine cascade completion from the loaded data
	type boolUpdate struct {
		table, idCol, field, id string
		value                   bool
	}
	var cascadeUpdates []boolUpdate

	allMilestones := roadmap.Milestones
	for _, p := range roadmap.Phases {
		allMilestones = append(allMilestones, p.Milestones...)
	}

	for i := range allMilestones {
		m := &allMilestones[i]
		shouldReach := len(m.Tasks) > 0
		for _, t := range m.Tasks {
			// Task completion: all steps done → completed (already set by caller),
			// but check in-memory completed flag against DB.
			if !t.Completed {
				shouldReach = false
			}
		}
		if m.Reached != shouldReach {
			cascadeUpdates = append(cascadeUpdates, boolUpdate{"milestones", "milestone_id", "reached", m.MilestoneID, shouldReach})
		}
	}

	allReached := len(allMilestones) > 0
	for _, m := range allMilestones {
		if !m.Reached {
			shouldReach := len(m.Tasks) > 0
			for _, t := range m.Tasks {
				if !t.Completed {
					shouldReach = false
					break
				}
			}
			if !shouldReach {
				allReached = false
				break
			}
		}
	}
	if roadmap.Completed != allReached {
		cascadeUpdates = append(cascadeUpdates, boolUpdate{"roadmaps", "roadmap_id", "completed", roadmap.RoadmapID, allReached})
	}

	// Collect status updates
	taskStatuses := make(map[string]string)
	msStatuses := make(map[string]string)
	phaseStatuses := make(map[string]string)

	for i := range roadmap.Phases {
		for j := range roadmap.Phases[i].Milestones {
			ms := &roadmap.Phases[i].Milestones[j]
			for _, t := range ms.Tasks {
				taskStatuses[t.TaskID] = t.Status
			}
			msStatuses[ms.MilestoneID] = ms.Status
		}
		phaseStatuses[roadmap.Phases[i].PhaseID] = roadmap.Phases[i].Status
	}
	for i := range roadmap.Milestones {
		ms := &roadmap.Milestones[i]
		for _, t := range ms.Tasks {
			taskStatuses[t.TaskID] = t.Status
		}
		msStatuses[ms.MilestoneID] = ms.Status
	}

	return db.Transaction(func(tx *gorm.DB) error {
		for _, u := range cascadeUpdates {
			if err := tx.Exec(
				fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", u.table, u.field, u.idCol),
				u.value, u.id,
			).Error; err != nil {
				return err
			}
		}
		if err := bulkUpdateStatus(tx, "tasks", "task_id", taskStatuses); err != nil {
			return err
		}
		if err := bulkUpdateStatus(tx, "milestones", "milestone_id", msStatuses); err != nil {
			return err
		}
		return bulkUpdateStatus(tx, "phases", "phase_id", phaseStatuses)
	})
}

func saveAllStatuses(roadmap *types.Roadmap) error {
	taskStatuses := make(map[string]string)
	msStatuses := make(map[string]string)
	phaseStatuses := make(map[string]string)

	for i := range roadmap.Phases {
		for j := range roadmap.Phases[i].Milestones {
			ms := &roadmap.Phases[i].Milestones[j]
			for _, t := range ms.Tasks {
				taskStatuses[t.TaskID] = t.Status
			}
			msStatuses[ms.MilestoneID] = ms.Status
		}
		phaseStatuses[roadmap.Phases[i].PhaseID] = roadmap.Phases[i].Status
	}
	for i := range roadmap.Milestones {
		ms := &roadmap.Milestones[i]
		if ms.PhaseID != nil && *ms.PhaseID != "" {
			continue
		}
		for _, t := range ms.Tasks {
			taskStatuses[t.TaskID] = t.Status
		}
		msStatuses[ms.MilestoneID] = ms.Status
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := bulkUpdateStatus(tx, "tasks", "task_id", taskStatuses); err != nil {
			return err
		}
		if err := bulkUpdateStatus(tx, "milestones", "milestone_id", msStatuses); err != nil {
			return err
		}
		return bulkUpdateStatus(tx, "phases", "phase_id", phaseStatuses)
	})
}

// RecomputeAndSaveStatusesByMilestone is a convenience wrapper that recomputes
// and saves statuses for the entire roadmap containing the given milestone.
func RecomputeAndSaveStatusesByMilestone(milestoneID string) error {
	var milestone types.Milestone
	if err := db.Where("milestone_id = ?", milestoneID).First(&milestone).Error; err != nil {
		return err
	}
	return RecomputeAndSaveStatuses(milestone.RoadmapID)
}

// UpdateAllStatuses recomputes and saves statuses for all roadmaps.
// This is intended to be called by a daily job to catch status changes as dates pass.
// Returns the number of tasks, milestones, and phases updated.
func UpdateAllStatuses() (tasksUpdated int64, milestonesUpdated int64, err error) {
	// Load all roadmaps with their nested data
	var roadmaps []types.Roadmap
	if err := db.Preload("Phases", func(db *gorm.DB) *gorm.DB {
		return db.Order("order_index ASC")
	}).Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("order_index ASC")
	}).Preload("Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Find(&roadmaps).Error; err != nil {
		return 0, 0, err
	}

	// Cache locations per user so a user with many roadmaps costs one lookup.
	locByUser := make(map[string]*time.Location)

	// Collect all status updates across all roadmaps, then flush in bulk.
	taskStatuses := make(map[string]string)
	msStatuses := make(map[string]string)
	phaseStatuses := make(map[string]string)

	for _, roadmap := range roadmaps {
		loc, ok := locByUser[roadmap.UserID]
		if !ok {
			loc = resolveUserLocation(roadmap.UserID)
			locByUser[roadmap.UserID] = loc
		}

		roadmap.ComputeAllStatusesInLocation(loc)

		for i := range roadmap.Phases {
			for j := range roadmap.Phases[i].Milestones {
				milestone := &roadmap.Phases[i].Milestones[j]
				for _, task := range milestone.Tasks {
					taskStatuses[task.TaskID] = task.Status
				}
				msStatuses[milestone.MilestoneID] = milestone.Status
			}
			phaseStatuses[roadmap.Phases[i].PhaseID] = roadmap.Phases[i].Status
		}

		for i := range roadmap.Milestones {
			milestone := &roadmap.Milestones[i]
			if milestone.PhaseID != nil && *milestone.PhaseID != "" {
				continue
			}
			for _, task := range milestone.Tasks {
				taskStatuses[task.TaskID] = task.Status
			}
			msStatuses[milestone.MilestoneID] = milestone.Status
		}
	}

	if err := bulkUpdateStatus(db, "tasks", "task_id", taskStatuses); err != nil {
		return 0, 0, err
	}
	if err := bulkUpdateStatus(db, "milestones", "milestone_id", msStatuses); err != nil {
		return 0, 0, err
	}
	if err := bulkUpdateStatus(db, "phases", "phase_id", phaseStatuses); err != nil {
		return 0, 0, err
	}

	log.Printf("UpdateAllStatuses: updated %d tasks, %d milestones, %d phases",
		len(taskStatuses), len(msStatuses), len(phaseStatuses))
	return int64(len(taskStatuses)), int64(len(msStatuses)), nil
}
