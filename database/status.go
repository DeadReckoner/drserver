package database

import (
	"log"
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

// RecomputeAndSaveStatuses loads a roadmap with all nested data, recomputes all statuses
// using Roadmap.ComputeAllStatuses(), and saves all statuses to the database.
// This is the single entry point for status recomputation.
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

	// Compute all statuses in memory, using the owner's timezone so the
	// overdue boundary matches their local day.
	roadmap.ComputeAllStatusesInLocation(resolveUserLocation(roadmap.UserID))

	// Save all statuses to database in a transaction
	return db.Transaction(func(tx *gorm.DB) error {
		// Save task statuses for milestones in phases
		for i := range roadmap.Phases {
			for j := range roadmap.Phases[i].Milestones {
				milestone := &roadmap.Phases[i].Milestones[j]
				for _, task := range milestone.Tasks {
					if err := tx.Model(&types.Task{}).
						Where("task_id = ?", task.TaskID).
						Update("status", task.Status).Error; err != nil {
						return err
					}
				}
				// Save milestone status
				if err := tx.Model(&types.Milestone{}).
					Where("milestone_id = ?", milestone.MilestoneID).
					Update("status", milestone.Status).Error; err != nil {
					return err
				}
			}
			// Save phase status
			if err := tx.Model(&types.Phase{}).
				Where("phase_id = ?", roadmap.Phases[i].PhaseID).
				Update("status", roadmap.Phases[i].Status).Error; err != nil {
				return err
			}
		}

		// Save task and milestone statuses for top-level milestones
		// Skip milestones that belong to a phase (already saved above)
		for i := range roadmap.Milestones {
			milestone := &roadmap.Milestones[i]
			if milestone.PhaseID != nil && *milestone.PhaseID != "" {
				continue // Already saved with phases
			}
			for _, task := range milestone.Tasks {
				if err := tx.Model(&types.Task{}).
					Where("task_id = ?", task.TaskID).
					Update("status", task.Status).Error; err != nil {
					return err
				}
			}
			// Save milestone status
			if err := tx.Model(&types.Milestone{}).
				Where("milestone_id = ?", milestone.MilestoneID).
				Update("status", milestone.Status).Error; err != nil {
				return err
			}
		}

		return nil
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
