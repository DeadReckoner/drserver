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
func generateUUIDs(rm *types.Roadmap) {
	rm.RoadmapID = uuid.NewString()

	if rm.MacroMap != nil {
		rm.MacroMap.MacroMapID = uuid.NewString()
		rm.MacroMap.RoadmapID = rm.RoadmapID

		for i := range rm.MacroMap.Phases {
			rm.MacroMap.Phases[i].PhaseID = uuid.NewString()
			rm.MacroMap.Phases[i].MacroMapID = rm.MacroMap.MacroMapID

			for j := range rm.MacroMap.Phases[i].MacroMilestones {
				rm.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID = uuid.NewString()
				rm.MacroMap.Phases[i].MacroMilestones[j].PhaseID = rm.MacroMap.Phases[i].PhaseID

				for k := range rm.MacroMap.Phases[i].MacroMilestones[j].Spikes {
					rm.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].SpikeID = uuid.NewString()
					rm.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].MacroMilestoneID = rm.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}

				for k := range rm.MacroMap.Phases[i].MacroMilestones[j].Risks {
					rm.MacroMap.Phases[i].MacroMilestones[j].Risks[k].RiskID = uuid.NewString()
					rm.MacroMap.Phases[i].MacroMilestones[j].Risks[k].MacroMilestoneID = rm.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}
			}
		}
	}

	for i := range rm.Milestones {
		rm.Milestones[i].MilestoneID = uuid.NewString()
		rm.Milestones[i].RoadmapID = rm.RoadmapID

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
		Preload("Milestones.Tasks.Steps")

	// Also preload macro map if it was created
	if rm.MacroMap != nil {
		query = query.
			Preload("MacroMap.Phases.MacroMilestones.Spikes").
			Preload("MacroMap.Phases.MacroMilestones.Risks")
	}

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

// GetRoadmapWithMacroMap loads only the Macro Map (strategic overview) for a roadmap
func GetRoadmapWithMacroMap(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones.Spikes").
		Preload("MacroMap.Phases.MacroMilestones.Risks").
		First(&roadmap)

	if result.Error != nil {
		log.Println("Error fetching roadmap macro map:", result.Error)
	}

	return roadmap
}

// GetRoadmapWithMacroMapAndMilestones loads both Macro Map and Milestone Map (complete view)
func GetRoadmapWithMacroMapAndMilestones(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.MacroMilestones.Spikes").
		Preload("MacroMap.Phases.MacroMilestones.Risks").
		Preload("Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
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
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).Preload("Milestones", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
		return db.Order("day ASC")
	}).Preload("Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
		return db.Order("number ASC")
	}).Find(&roadmap)
	if result.Error != nil {
		log.Println("Error fetching roadmaps:", result.Error)
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
				roadmap.StartDate = value.(time.Time)
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

// CreateMacroMap creates a macro map with all its phases, milestones, spikes, and risks
func CreateMacroMap(c *gin.Context, roadmapID string, macroMap types.MacroMap) (types.MacroMap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.MacroMap{}, errors.New("missing or invalid user_id")
	}

	// Verify roadmap exists and belongs to user
	var roadmap types.Roadmap
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).First(&roadmap)
	if result.Error != nil {
		return types.MacroMap{}, errors.New("roadmap not found or access denied")
	}

	macroMap.RoadmapID = roadmapID

	// Create the macro map with all nested data
	tx := db.Create(&macroMap)
	if tx.Error != nil {
		return types.MacroMap{}, tx.Error
	}

	// Fetch the created macro map with all associations
	var createdMacroMap types.MacroMap
	if err := db.Where("macro_map_id = ?", macroMap.MacroMapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones.Spikes").
		Preload("Phases.MacroMilestones.Risks").
		First(&createdMacroMap).Error; err != nil {
		return types.MacroMap{}, err
	}

	return createdMacroMap, nil
}

// GetMacroMap fetches a macro map by roadmap ID
func GetMacroMap(c *gin.Context, roadmapID string) (types.MacroMap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.MacroMap{}, errors.New("missing or invalid user_id")
	}

	// Verify roadmap exists and belongs to user
	var roadmap types.Roadmap
	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).First(&roadmap)
	if result.Error != nil {
		return types.MacroMap{}, errors.New("roadmap not found or access denied")
	}

	var macroMap types.MacroMap
	err := db.Where("roadmap_id = ?", roadmapID).
		Preload("Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("Phases.MacroMilestones.Spikes").
		Preload("Phases.MacroMilestones.Risks").
		First(&macroMap).Error

	if err != nil {
		return types.MacroMap{}, err
	}

	return macroMap, nil
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
	// Generate UUIDs for all nested entities (MacroMap, Phases, etc.)
	// Note: RoadmapID should already be set from the draft
	if roadmap.MacroMap != nil {
		roadmap.MacroMap.MacroMapID = uuid.NewString()
		roadmap.MacroMap.RoadmapID = roadmap.RoadmapID

		for i := range roadmap.MacroMap.Phases {
			roadmap.MacroMap.Phases[i].PhaseID = uuid.NewString()
			roadmap.MacroMap.Phases[i].MacroMapID = roadmap.MacroMap.MacroMapID

			for j := range roadmap.MacroMap.Phases[i].MacroMilestones {
				roadmap.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID = uuid.NewString()
				roadmap.MacroMap.Phases[i].MacroMilestones[j].PhaseID = roadmap.MacroMap.Phases[i].PhaseID

				for k := range roadmap.MacroMap.Phases[i].MacroMilestones[j].Spikes {
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].SpikeID = uuid.NewString()
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Spikes[k].MacroMilestoneID = roadmap.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}

				for k := range roadmap.MacroMap.Phases[i].MacroMilestones[j].Risks {
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Risks[k].RiskID = uuid.NewString()
					roadmap.MacroMap.Phases[i].MacroMilestones[j].Risks[k].MacroMilestoneID = roadmap.MacroMap.Phases[i].MacroMilestones[j].MacroMilestoneID
				}
			}
		}
	}

	// Handle milestones if they exist (though async generation currently only creates MacroMap)
	for i := range roadmap.Milestones {
		roadmap.Milestones[i].MilestoneID = uuid.NewString()
		roadmap.Milestones[i].RoadmapID = roadmap.RoadmapID
		roadmap.Milestones[i].UserID = roadmap.UserID

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
	// FullSaveAssociations ensures nested entities (MacroMap, Phases, etc.) are saved
	return db.Session(&gorm.Session{FullSaveAssociations: true}).Updates(roadmap).Error
}

func UpdateRoadmapGenerationStatus(ctx context.Context, roadmapID, status string) error {
	return db.Model(&types.Roadmap{}).
		Where("roadmap_id = ?", roadmapID).
		Update("generation_status", status).Error
}
