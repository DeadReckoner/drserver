package database

import (
	"context"
	"errors"
	"log"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// generateUUIDs generates UUIDs for all entities in the roadmap.
// The frontend sends empty strings for UUIDs, but GORM/PostgreSQL can't handle empty strings for UUID columns.
// We generate UUIDs in Go and set all foreign keys before inserting.
//
// New structure: Roadmap → Phases → Milestones → Spikes/Risks/Tasks → Steps
func generateUUIDs(rm *types.Roadmap) {
	rm.RoadmapID = uuid.NewString()

	// Generate UUIDs for Phases and their nested Milestones
	for i := range rm.Phases {
		rm.Phases[i].PhaseID = uuid.NewString()
		rm.Phases[i].RoadmapID = rm.RoadmapID

		for j := range rm.Phases[i].Milestones {
			milestoneID := uuid.NewString()
			rm.Phases[i].Milestones[j].MilestoneID = milestoneID
			rm.Phases[i].Milestones[j].RoadmapID = rm.RoadmapID
			phaseID := rm.Phases[i].PhaseID
			rm.Phases[i].Milestones[j].PhaseID = &phaseID

			// Generate UUIDs for Spikes
			for k := range rm.Phases[i].Milestones[j].Spikes {
				rm.Phases[i].Milestones[j].Spikes[k].SpikeID = uuid.NewString()
				rm.Phases[i].Milestones[j].Spikes[k].MilestoneID = milestoneID
			}

			// Generate UUIDs for Risks
			for k := range rm.Phases[i].Milestones[j].Risks {
				rm.Phases[i].Milestones[j].Risks[k].RiskID = uuid.NewString()
				rm.Phases[i].Milestones[j].Risks[k].MilestoneID = milestoneID
			}

			// Generate UUIDs for Tasks and Steps
			for k := range rm.Phases[i].Milestones[j].Tasks {
				rm.Phases[i].Milestones[j].Tasks[k].TaskID = uuid.NewString()
				rm.Phases[i].Milestones[j].Tasks[k].MilestoneID = milestoneID
				rm.Phases[i].Milestones[j].Tasks[k].RoadmapID = rm.RoadmapID

				for l := range rm.Phases[i].Milestones[j].Tasks[k].Steps {
					rm.Phases[i].Milestones[j].Tasks[k].Steps[l].StepID = uuid.NewString()
					rm.Phases[i].Milestones[j].Tasks[k].Steps[l].TaskID = rm.Phases[i].Milestones[j].Tasks[k].TaskID
				}
			}
		}
	}

	// Also handle top-level Milestones array (for backward compatibility)
	for i := range rm.Milestones {
		if rm.Milestones[i].MilestoneID == "" {
			rm.Milestones[i].MilestoneID = uuid.NewString()
		}
		rm.Milestones[i].RoadmapID = rm.RoadmapID

		// Generate UUIDs for Spikes attached directly to top-level milestones
		for k := range rm.Milestones[i].Spikes {
			rm.Milestones[i].Spikes[k].SpikeID = uuid.NewString()
			rm.Milestones[i].Spikes[k].MilestoneID = rm.Milestones[i].MilestoneID
		}

		// Generate UUIDs for Risks attached directly to top-level milestones
		for k := range rm.Milestones[i].Risks {
			rm.Milestones[i].Risks[k].RiskID = uuid.NewString()
			rm.Milestones[i].Risks[k].MilestoneID = rm.Milestones[i].MilestoneID
		}

		for j := range rm.Milestones[i].Tasks {
			rm.Milestones[i].Tasks[j].TaskID = uuid.NewString()
			rm.Milestones[i].Tasks[j].MilestoneID = rm.Milestones[i].MilestoneID
			rm.Milestones[i].Tasks[j].RoadmapID = rm.RoadmapID

			for k := range rm.Milestones[i].Tasks[j].Steps {
				rm.Milestones[i].Tasks[j].Steps[k].StepID = uuid.NewString()
				rm.Milestones[i].Tasks[j].Steps[k].TaskID = rm.Milestones[i].Tasks[j].TaskID
			}
		}
	}
}

func CreateRoadmap(c *gin.Context, rm types.Roadmap) (types.Roadmap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	userIDStr := user_id.(string)

	// Ensure user exists in database before creating roadmap
	var existingUser types.User
	userResult := db.Where("user_id = ?", userIDStr).First(&existingUser)
	if userResult.Error == gorm.ErrRecordNotFound {
		user := types.User{
			UserID:    userIDStr,
			CreatedAt: time.Now(),
		}
		if err := db.Create(&user).Error; err != nil {
			return types.Roadmap{}, errors.New("failed to create user: " + err.Error())
		}
	} else if userResult.Error != nil {
		return types.Roadmap{}, errors.New("failed to check user existence: " + userResult.Error.Error())
	}

	// Generate UUIDs for all entities and set foreign keys
	generateUUIDs(&rm)

	// Set user IDs for all nested entities
	rm.UserID = userIDStr
	for i := range rm.Milestones {
		rm.Milestones[i].UserID = userIDStr
		for j := range rm.Milestones[i].Tasks {
			rm.Milestones[i].Tasks[j].UserID = userIDStr
		}
	}

	tx := db.Create(&rm)
	if tx.Error != nil {
		return types.Roadmap{}, tx.Error
	}

	var createdRoadmap types.Roadmap
	query := db.Where("roadmap_id = ?", rm.RoadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones.Spikes").
		Preload("Phases.Milestones.Risks").
		Preload("Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Spikes").
		Preload("Milestones.Risks").
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		})

	if err := query.First(&createdRoadmap).Error; err != nil {
		return types.Roadmap{}, err
	}

	return createdRoadmap, nil
}

func GetRoadmapsByUser(c *gin.Context) []types.Roadmap {
	var roadmaps []types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmaps
	}

	// Query the roadmap table for all roadmaps with this user_id
	result := db.Where("user_id = ?", user_id).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&roadmaps)
	if result.Error != nil {
		log.Println("Error fetching roadmaps:", result.Error)
	}

	return roadmaps
}

// GetRoadmapWithStrategicView loads only the strategic overview (phases with milestones, no tasks)
func GetRoadmapWithStrategicView(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones.Spikes").
		Preload("Phases.Milestones.Risks").
		First(&roadmap)

	if result.Error != nil {
		log.Println("Error fetching roadmap strategic view:", result.Error)
	}

	return roadmap
}

// GetRoadmapFull loads the complete roadmap with phases, milestones, tasks, and steps
func GetRoadmapFull(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.Milestones.Spikes").
		Preload("Phases.Milestones.Risks").
		Preload("Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Spikes").
		Preload("Milestones.Risks").
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		First(&roadmap)

	if result.Error != nil {
		log.Println("Error fetching full roadmap:", result.Error)
	}

	return roadmap
}

func GetRoadmap(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	// Query the roadmap table for all roadmaps with this user_id
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Spikes").
		Preload("Milestones.Risks").
		Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).Find(&roadmap)
	if result.Error != nil {
		log.Println("Error fetching roadmap:", result.Error)
	}

	return roadmap
}

func UpdateRoadmap(c *gin.Context, roadmapID string, rm types.Roadmap) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	// Fetch the existing roadmap
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Preload("Milestones.Tasks.Steps").First(&roadmap)
	if result.Error != nil {
		log.Println("Error fetching roadmap:", result.Error)
		return roadmap
	}

	// Use GORM's Updates method to only update non-zero fields
	updateMap := make(map[string]interface{})
	if rm.Goal != "" {
		updateMap["goal"] = rm.Goal
	}
	if rm.Description != "" {
		updateMap["description"] = rm.Description
	}
	if rm.Days != 0 {
		updateMap["days"] = rm.Days
	}
	// Handle boolean field explicitly since Updates ignores zero values
	if rm.Completed != roadmap.Completed {
		updateMap["completed"] = rm.Completed
	}
	if rm.StartDate != roadmap.StartDate {
		updateMap["start_date"] = rm.StartDate
	}
	if rm.CurrentSkillLevel != "" {
		updateMap["current_skill_level"] = rm.CurrentSkillLevel
	}
	// if !rm.StartDate.IsZero() {
	// 	updateMap["start_date"] = rm.StartDate
	// }

	if len(updateMap) > 0 {
		updateResult := db.Model(&roadmap).Updates(updateMap)
		if updateResult.Error != nil {
			log.Println("Error updating roadmap:", updateResult.Error)
			return roadmap
		}
		// Update the roadmap struct with the new values
		for key, value := range updateMap {
			switch key {
			case "goal":
				roadmap.Goal = value.(string)
			case "description":
				roadmap.Description = value.(string)
			case "days":
				roadmap.Days = value.(int)
			case "completed":
				roadmap.Completed = value.(bool)
			case "start_date":
				roadmap.StartDate = value.(*time.Time)
			case "current_skill_level":
				roadmap.CurrentSkillLevel = value.(string)
			}
		}
	}

	// Check if all milestones are completed to auto-complete roadmap
	if shouldCompleteRoadmap(roadmap.RoadmapID) {
		db.Model(&roadmap).Update("completed", true)
		roadmap.Completed = true
	}

	// Fetch updated roadmap with associations
	db.Where("roadmap_id = ?", roadmapID).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&roadmap)

	return roadmap
}

func DeleteRoadmap(c *gin.Context, roadmapID string) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	// Delete the roadmap for this user and roadmapID
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Delete(&types.Roadmap{})
	if result.Error != nil {
		log.Println("Error deleting roadmap:", result.Error)
		return result.Error
	}

	return nil
}

func GetMilestonesByRoadmap(c *gin.Context, roadmapID string) []types.Milestone {
	var milestones []types.Milestone
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return []types.Milestone{}
	}

	results := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Order("day ASC").Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&milestones)

	if results.Error != nil {
		log.Println("Error fetching milestones:", results.Error)
	}

	return milestones
}

func GetMilestone(c *gin.Context, milestoneID string) types.Milestone {
	var milestone types.Milestone
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}
	}

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&milestone)

	if result.Error != nil {
		log.Println("Error fetching milestone:", result.Error)
	}

	return milestone
}

func UpdateMilestone(c *gin.Context, milestoneID string, updatedMilestone types.Milestone) types.Milestone {
	var milestone types.Milestone
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}
	}

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&milestone)

	if result.Error != nil {
		log.Println("Error fetching milestone:", result.Error)
		return types.Milestone{}
	}

	// Use GORM's Updates method to only update non-zero fields
	updateMap := make(map[string]interface{})
	if updatedMilestone.SubGoal != "" {
		updateMap["sub_goal"] = updatedMilestone.SubGoal
	}
	if updatedMilestone.Description != "" {
		updateMap["description"] = updatedMilestone.Description
	}
	if updatedMilestone.Day != 0 {
		updateMap["day"] = updatedMilestone.Day
	}
	// Handle boolean field explicitly since Updates ignores zero values
	if updatedMilestone.Reached != milestone.Reached {
		updateMap["reached"] = updatedMilestone.Reached
	}
	if updatedMilestone.Date != milestone.Date {
		updateMap["date"] = updatedMilestone.Date
	}

	if len(updateMap) > 0 {
		updateResult := db.Model(&milestone).Updates(updateMap)
		if updateResult.Error != nil {
			log.Println("Error updating milestone:", updateResult.Error)
			return types.Milestone{}
		}
		// Update the milestone struct with the new values
		for key, value := range updateMap {
			switch key {
			case "sub_goal":
				milestone.SubGoal = value.(string)
			case "description":
				milestone.Description = value.(string)
			case "day":
				milestone.Day = value.(int)
			case "reached":
				milestone.Reached = value.(bool)
			}
		}
	}

	// Check if all tasks are completed to auto-complete milestone
	if shouldCompleteMilestone(milestone.MilestoneID) {
		db.Model(&milestone).Update("reached", true)
		milestone.Reached = true

		// Check if this completion should trigger roadmap completion
		if shouldCompleteRoadmap(milestone.RoadmapID) {
			db.Model(&types.Roadmap{}).Where("roadmap_id = ?", milestone.RoadmapID).Update("completed", true)
		}
	}

	// Fetch updated milestone with associations
	db.Where("milestone_id = ?", milestoneID).Preload("Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).First(&milestone)

	return milestone
}

func CreateMilestone(c *gin.Context, roadmapID string, milestone types.Milestone) (types.Milestone, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}, errors.New("missing or invalid user_id")
	}

	milestone.UserID = user_id.(string)
	milestone.RoadmapID = roadmapID

	for i := range milestone.Tasks {
		milestone.Tasks[i].UserID = milestone.UserID
		milestone.Tasks[i].RoadmapID = milestone.RoadmapID
		milestone.Tasks[i].MilestoneID = milestone.MilestoneID
		for j := range milestone.Tasks[i].Steps {
			milestone.Tasks[i].Steps[j].TaskID = milestone.Tasks[i].TaskID
		}
	}

	tx := db.Create(&milestone)
	if tx.Error != nil {
		return types.Milestone{}, tx.Error
	}

	var createdMilestone types.Milestone
	if err := db.Where("milestone_id = ?", milestone.MilestoneID).Preload("Tasks.Steps").First(&createdMilestone).Error; err != nil {
		return types.Milestone{}, err
	}

	return createdMilestone, nil
}

func DeleteMilestone(c *gin.Context, milestoneID string) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	result := db.Where("user_id = ?", user_id).Where("milestone_id = ?", milestoneID).Delete(&types.Milestone{})
	if result.Error != nil {
		log.Println("Error deleting milestone:", result.Error)
		return result.Error
	}

	return nil
}

func CreateDraftRoadmap(c *gin.Context, draft *types.Roadmap) error {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return errors.New("missing or invalid user_id")
	}

	userIDStr := user_id.(string)

	// Ensure user exists in database
	var existingUser types.User
	userResult := db.Where("user_id = ?", userIDStr).First(&existingUser)
	if userResult.Error == gorm.ErrRecordNotFound {
		user := types.User{
			UserID:    userIDStr,
			CreatedAt: time.Now(),
		}
		if err := db.Create(&user).Error; err != nil {
			return errors.New("failed to create user: " + err.Error())
		}
	} else if userResult.Error != nil {
		return errors.New("failed to check user existence: " + userResult.Error.Error())
	}

	// Set user ID (should already be set, but ensure it)
	draft.UserID = userIDStr

	// Create the draft roadmap (no nested structures yet)
	return db.Create(draft).Error
}

func UpdateRoadmapWithGeneration(ctx context.Context, roadmap *types.Roadmap) error {
	// Generate UUIDs for all nested entities (Phases, Milestones, etc.)
	// Note: RoadmapID should already be set from the draft

	// Handle Phases and their nested Milestones
	for i := range roadmap.Phases {
		roadmap.Phases[i].PhaseID = uuid.NewString()
		roadmap.Phases[i].RoadmapID = roadmap.RoadmapID

		for j := range roadmap.Phases[i].Milestones {
			milestoneID := uuid.NewString()
			roadmap.Phases[i].Milestones[j].MilestoneID = milestoneID
			roadmap.Phases[i].Milestones[j].RoadmapID = roadmap.RoadmapID
			roadmap.Phases[i].Milestones[j].UserID = roadmap.UserID
			phaseID := roadmap.Phases[i].PhaseID
			roadmap.Phases[i].Milestones[j].PhaseID = &phaseID

			// Generate UUIDs for Spikes
			for k := range roadmap.Phases[i].Milestones[j].Spikes {
				roadmap.Phases[i].Milestones[j].Spikes[k].SpikeID = uuid.NewString()
				roadmap.Phases[i].Milestones[j].Spikes[k].MilestoneID = milestoneID
			}

			// Generate UUIDs for Risks
			for k := range roadmap.Phases[i].Milestones[j].Risks {
				roadmap.Phases[i].Milestones[j].Risks[k].RiskID = uuid.NewString()
				roadmap.Phases[i].Milestones[j].Risks[k].MilestoneID = milestoneID
			}

			// Generate UUIDs for Tasks and Steps
			for k := range roadmap.Phases[i].Milestones[j].Tasks {
				roadmap.Phases[i].Milestones[j].Tasks[k].TaskID = uuid.NewString()
				roadmap.Phases[i].Milestones[j].Tasks[k].MilestoneID = milestoneID
				roadmap.Phases[i].Milestones[j].Tasks[k].RoadmapID = roadmap.RoadmapID
				roadmap.Phases[i].Milestones[j].Tasks[k].UserID = roadmap.UserID

				for l := range roadmap.Phases[i].Milestones[j].Tasks[k].Steps {
					roadmap.Phases[i].Milestones[j].Tasks[k].Steps[l].StepID = uuid.NewString()
					roadmap.Phases[i].Milestones[j].Tasks[k].Steps[l].TaskID = roadmap.Phases[i].Milestones[j].Tasks[k].TaskID
				}
			}
		}
	}

	// Handle top-level Milestones (for backward compatibility)
	for i := range roadmap.Milestones {
		if roadmap.Milestones[i].MilestoneID == "" {
			roadmap.Milestones[i].MilestoneID = uuid.NewString()
		}
		roadmap.Milestones[i].RoadmapID = roadmap.RoadmapID
		roadmap.Milestones[i].UserID = roadmap.UserID

		// Generate UUIDs for Spikes
		for k := range roadmap.Milestones[i].Spikes {
			roadmap.Milestones[i].Spikes[k].SpikeID = uuid.NewString()
			roadmap.Milestones[i].Spikes[k].MilestoneID = roadmap.Milestones[i].MilestoneID
		}

		// Generate UUIDs for Risks
		for k := range roadmap.Milestones[i].Risks {
			roadmap.Milestones[i].Risks[k].RiskID = uuid.NewString()
			roadmap.Milestones[i].Risks[k].MilestoneID = roadmap.Milestones[i].MilestoneID
		}

		for j := range roadmap.Milestones[i].Tasks {
			roadmap.Milestones[i].Tasks[j].TaskID = uuid.NewString()
			roadmap.Milestones[i].Tasks[j].MilestoneID = roadmap.Milestones[i].MilestoneID
			roadmap.Milestones[i].Tasks[j].RoadmapID = roadmap.RoadmapID
			roadmap.Milestones[i].Tasks[j].UserID = roadmap.UserID

			for k := range roadmap.Milestones[i].Tasks[j].Steps {
				roadmap.Milestones[i].Tasks[j].Steps[k].StepID = uuid.NewString()
				roadmap.Milestones[i].Tasks[j].Steps[k].TaskID = roadmap.Milestones[i].Tasks[j].TaskID
			}
		}
	}

	// Update the roadmap with full save of all associations
	// FullSaveAssociations ensures nested entities (Phases, Milestones, etc.) are saved
	return db.Session(&gorm.Session{FullSaveAssociations: true}).Updates(roadmap).Error
}

func UpdateRoadmapGenerationStatus(ctx context.Context, roadmapID, status string) error {
	return db.Model(&types.Roadmap{}).
		Where("roadmap_id = ?", roadmapID).
		Update("generation_status", status).Error
}
