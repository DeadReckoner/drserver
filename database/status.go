package database

import (
	"log"
	"sort"
	"time"

	"dr/server/types"

	"gorm.io/gorm"
)

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

// UserLocation exposes resolveUserLocation to callers outside this package, so
// handlers that schedule dates can anchor them to the user's calendar day the
// same way status computation judges the overdue boundary.
func UserLocation(userID string) *time.Location {
	return resolveUserLocation(userID)
}

// statusRow is one row whose status column RecomputeAndSaveStatuses persists.
// cur points at the Status field inside the loaded roadmap, which
// ComputeAllStatusesInLocation rewrites in place — so snapshotting cur before
// the compute and reading it after is what tells us the row actually changed.
type statusRow struct {
	id  string
	cur *string
}

// collectStatusRows returns every task, milestone and phase row whose status
// RecomputeAndSaveStatuses owns.
//
// Milestones that belong to a phase are reached through roadmap.Phases. The
// top-level roadmap.Milestones preload loads those same DB rows a second time
// (into separate structs), so phase-owned entries are skipped there — without
// that check each phase-owned milestone and its tasks would be written twice.
func collectStatusRows(r *types.Roadmap) (tasks, milestones, phases []statusRow) {
	collectMilestone := func(m *types.Milestone) {
		for k := range m.Tasks {
			t := &m.Tasks[k]
			tasks = append(tasks, statusRow{id: t.TaskID, cur: &t.Status})
		}
		milestones = append(milestones, statusRow{id: m.MilestoneID, cur: &m.Status})
	}

	for i := range r.Phases {
		p := &r.Phases[i]
		for j := range p.Milestones {
			collectMilestone(&p.Milestones[j])
		}
		phases = append(phases, statusRow{id: p.PhaseID, cur: &p.Status})
	}

	for i := range r.Milestones {
		m := &r.Milestones[i]
		if m.PhaseID != nil && *m.PhaseID != "" {
			continue // already collected via r.Phases
		}
		collectMilestone(m)
	}

	return tasks, milestones, phases
}

// snapshot copies the current status of each row, for diffing after compute.
func snapshot(rows []statusRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = *r.cur
	}
	return out
}

// changedByStatus groups the ids whose status differs from prev, keyed by the
// new status. Rows that didn't change contribute nothing, so an unchanged
// roadmap produces no writes at all.
func changedByStatus(rows []statusRow, prev []string) map[string][]string {
	byStatus := make(map[string][]string)
	for i, r := range rows {
		if r.id == "" || *r.cur == prev[i] {
			continue
		}
		byStatus[*r.cur] = append(byStatus[*r.cur], r.id)
	}
	return byStatus
}

// writeStatusGroups issues one UPDATE ... WHERE <pk> IN (...) per distinct
// status. Statuses are a small closed set, so a roadmap of any size costs at
// most a handful of statements per table rather than one per row.
//
// Groups are written in sorted status order to keep the emitted SQL stable —
// worth it for reading query logs, and it makes the statement sequence
// deterministic for tests.
func writeStatusGroups(tx *gorm.DB, model interface{}, pk string, byStatus map[string][]string) error {
	statuses := make([]string, 0, len(byStatus))
	for s := range byStatus {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)

	for _, s := range statuses {
		if err := tx.Model(model).
			Where(pk+" IN ?", byStatus[s]).
			Update("status", s).Error; err != nil {
			return err
		}
	}
	return nil
}

// RecomputeAndSaveStatuses loads a roadmap with all nested data, recomputes all statuses
// using Roadmap.ComputeAllStatuses(), and saves all statuses to the database.
// This is the single entry point for status recomputation.
//
// It runs on the request path — completing the last open step of a task
// cascades into here (see UpdateStep) — so it only writes rows whose status
// actually changed, and batches those into one UPDATE per distinct status.
// Completing a step typically flips two or three statuses; writing every task,
// milestone and phase in the roadmap one row at a time made a single step
// update cost hundreds of sequential round trips on a large roadmap.
func RecomputeAndSaveStatuses(roadmapID string) error {
	var roadmap types.Roadmap
	if err := db.Where("roadmap_id = ?", roadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).First(&roadmap).Error; err != nil {
		return err
	}

	// Snapshot what's stored before the compute overwrites it in place.
	tasks, milestones, phases := collectStatusRows(&roadmap)
	prevTasks := snapshot(tasks)
	prevMilestones := snapshot(milestones)
	prevPhases := snapshot(phases)

	// Compute all statuses in memory, using the owner's timezone so the
	// overdue boundary matches their local day.
	roadmap.ComputeAllStatusesInLocation(resolveUserLocation(roadmap.UserID))

	taskGroups := changedByStatus(tasks, prevTasks)
	milestoneGroups := changedByStatus(milestones, prevMilestones)
	phaseGroups := changedByStatus(phases, prevPhases)

	// Nothing moved — skip the transaction entirely rather than paying for a
	// BEGIN/COMMIT round trip that would wrap zero statements.
	if len(taskGroups) == 0 && len(milestoneGroups) == 0 && len(phaseGroups) == 0 {
		return nil
	}

	// Save the changed statuses to the database in a transaction
	return db.Transaction(func(tx *gorm.DB) error {
		if err := writeStatusGroups(tx, &types.Task{}, "task_id", taskGroups); err != nil {
			return err
		}
		if err := writeStatusGroups(tx, &types.Milestone{}, "milestone_id", milestoneGroups); err != nil {
			return err
		}
		return writeStatusGroups(tx, &types.Phase{}, "phase_id", phaseGroups)
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

	// Process each roadmap
	for _, roadmap := range roadmaps {
		loc, ok := locByUser[roadmap.UserID]
		if !ok {
			loc = resolveUserLocation(roadmap.UserID)
			locByUser[roadmap.UserID] = loc
		}

		// Compute all statuses in memory, using the owner's timezone.
		roadmap.ComputeAllStatusesInLocation(loc)

		// Save statuses for milestones in phases
		for i := range roadmap.Phases {
			for j := range roadmap.Phases[i].Milestones {
				milestone := &roadmap.Phases[i].Milestones[j]
				for _, task := range milestone.Tasks {
					result := db.Model(&types.Task{}).
						Where("task_id = ? AND status != ?", task.TaskID, task.Status).
						Update("status", task.Status)
					if result.Error != nil {
						return tasksUpdated, milestonesUpdated, result.Error
					}
					tasksUpdated += result.RowsAffected
				}

				result := db.Model(&types.Milestone{}).
					Where("milestone_id = ? AND status != ?", milestone.MilestoneID, milestone.Status).
					Update("status", milestone.Status)
				if result.Error != nil {
					return tasksUpdated, milestonesUpdated, result.Error
				}
				milestonesUpdated += result.RowsAffected
			}

			// Update phase status
			db.Model(&types.Phase{}).
				Where("phase_id = ? AND status != ?", roadmap.Phases[i].PhaseID, roadmap.Phases[i].Status).
				Update("status", roadmap.Phases[i].Status)
		}

		// Save statuses for top-level milestones
		// Skip milestones that belong to a phase (already saved above)
		for i := range roadmap.Milestones {
			milestone := &roadmap.Milestones[i]
			if milestone.PhaseID != nil && *milestone.PhaseID != "" {
				continue // Already saved with phases
			}
			for _, task := range milestone.Tasks {
				result := db.Model(&types.Task{}).
					Where("task_id = ? AND status != ?", task.TaskID, task.Status).
					Update("status", task.Status)
				if result.Error != nil {
					return tasksUpdated, milestonesUpdated, result.Error
				}
				tasksUpdated += result.RowsAffected
			}

			result := db.Model(&types.Milestone{}).
				Where("milestone_id = ? AND status != ?", milestone.MilestoneID, milestone.Status).
				Update("status", milestone.Status)
			if result.Error != nil {
				return tasksUpdated, milestonesUpdated, result.Error
			}
			milestonesUpdated += result.RowsAffected
		}
	}

	log.Printf("UpdateAllStatuses: updated %d tasks and %d milestones", tasksUpdated, milestonesUpdated)
	return tasksUpdated, milestonesUpdated, nil
}
