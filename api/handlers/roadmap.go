package handlers

import (
	"dr/server/database"
	"dr/server/types"
	"fmt"
	"log"
	"net/http"
	"time"

	"dr/server/api/utils"

	"github.com/gin-gonic/gin"
)

func CreateRoadmap(c *gin.Context) {
	var newRoadmap types.Roadmap

	if !utils.BindJSONOrAbort(c, &newRoadmap) {
		return
	}

	fmt.Printf("Roadmap received: %+v\n", newRoadmap)

	createdRoadmap, err := database.CreateRoadmap(c, newRoadmap)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdRoadmap)
	log.Println("Roadmap created successfully")
}

func GetRoadmapsByUser(c *gin.Context) {
	roadmaps := database.GetRoadmapsByUser(c)
	utils.DataResponse(c, http.StatusOK, roadmaps)
}

func GetRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmap(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
}

func UpdateRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	var updatedRoadmap types.Roadmap

	if !utils.BindJSONOrAbort(c, &updatedRoadmap) {
		return
	}

	fmt.Printf("Updated roadmap received: %+v\n", updatedRoadmap)

	roadmap := database.UpdateRoadmap(c, roadmapID, updatedRoadmap)
	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
	log.Println("Roadmap updated successfully")
}

func DeleteRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")

	err := database.DeleteRoadmap(c, roadmapID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Roadmap deleted successfully")
	log.Println("Roadmap deleted successfully")
}

func GetMilestonesByRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	milestones := database.GetMilestonesByRoadmap(c, roadmapID)
	utils.DataResponse(c, http.StatusOK, milestones)
}

func GetMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	milestone := database.GetMilestone(c, milestoneID)

	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	utils.DataResponse(c, http.StatusOK, milestone)
}

func UpdateMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")
	var updatedMilestone types.Milestone

	if !utils.BindJSONOrAbort(c, &updatedMilestone) {
		return
	}

	milestone := database.UpdateMilestone(c, milestoneID, updatedMilestone)
	if milestone.MilestoneID == "" {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	utils.DataResponse(c, http.StatusOK, milestone)
	log.Println("Milestone updated successfully")
}

func CreateMilestone(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	var newMilestone types.Milestone

	if !utils.BindJSONOrAbort(c, &newMilestone) {
		return
	}

	fmt.Printf("Milestone received: %+v\n", newMilestone)

	createdMilestone, err := database.CreateMilestone(c, roadmapID, newMilestone)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdMilestone)
	log.Println("Milestone created successfully")
}

func DeleteMilestone(c *gin.Context) {
	milestoneID := c.Param("milestoneid")

	err := database.DeleteMilestone(c, milestoneID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Milestone deleted successfully")
	log.Println("Milestone deleted successfully")
}

func StartRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmap(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	roadmap.StartDate = time.Now()

	for index, milestone := range roadmap.Milestones {

		fmt.Println("starting milestone ", milestone.Day)
		roadmap.Milestones[index] = dateMilestones(c, roadmap.StartDate, milestone)
		for taskIndex, task := range milestone.Tasks {
			fmt.Println("starting task ", task.Day)
			roadmap.Milestones[index].Tasks[taskIndex] = dateTasks(c, roadmap.StartDate, task)
		}
	}

	updatedRoadmap := database.UpdateRoadmap(c, roadmapID, roadmap)

	utils.DataResponse(c, http.StatusOK, updatedRoadmap)
	log.Println("Roadmap started successfully")
}

func dateMilestones(c *gin.Context, startDate time.Time, milestone types.Milestone) types.Milestone {

	milestone.Date = startDate.AddDate(0, 0, milestone.Day)

	return database.UpdateMilestone(c, milestone.MilestoneID, milestone)
}

func dateTasks(c *gin.Context, startDate time.Time, task types.Task) types.Task {

	task.Date = startDate.AddDate(0, 0, task.Day)

	return database.UpdateTask(c, task.TaskID, task)
}

// Macro Map handlers

func CreateMacroMap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	var newMacroMap types.MacroMap

	if !utils.BindJSONOrAbort(c, &newMacroMap) {
		return
	}

	fmt.Printf("Macro map received: %+v\n", newMacroMap)

	createdMacroMap, err := database.CreateMacroMap(c, roadmapID, newMacroMap)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, createdMacroMap)
	log.Println("Macro map created successfully")
}

func GetMacroMap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	macroMap, err := database.GetMacroMap(c, roadmapID)

	if err != nil {
		utils.NotFoundResponse(c, "Macro Map")
		return
	}

	utils.DataResponse(c, http.StatusOK, macroMap)
}

func GetRoadmapMacroView(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmapMacroMap(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
}

func GetRoadmapMilestoneView(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmapMilestoneMap(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
}

func GetRoadmapFullView(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	roadmap := database.GetRoadmapFull(c, roadmapID)

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
}

func LinkMacroMilestoneToMilestone(c *gin.Context) {
	var linkRequest struct {
		MacroMilestoneID string `json:"macro_milestone_id" binding:"required"`
		MilestoneID      string `json:"milestone_id" binding:"required"`
	}

	if !utils.BindJSONOrAbort(c, &linkRequest) {
		return
	}

	err := database.LinkMacroMilestoneToMilestone(linkRequest.MacroMilestoneID, linkRequest.MilestoneID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.MessageResponse(c, http.StatusOK, "Macro milestone linked to milestone successfully")
	log.Println("Macro milestone linked to milestone successfully")
}
