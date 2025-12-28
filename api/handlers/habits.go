package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetHabit(c *gin.Context) {
	habitID := c.Param("habitid")
	habit := database.GetHabit(c, habitID)

	if habit.HabitID == "" {
		utils.NotFoundResponse(c, "Habit")
		return
	}

	utils.DataResponse(c, http.StatusOK, habit)
}

func CreateHabit(c *gin.Context) {
	ctx := c.Request.Context()
	var habit types.Habit
	if !utils.BindJSONOrAbort(c, &habit) {
		//TODO Need to handle error
		return
	}

	createdHabit, err := database.CreateHabit(c, habit)

	if err != nil {
		return
	}

	utils.DataResponse(c, http.StatusOK, createdHabit)
	log.Info(ctx, "Habit Created Successfully")
}

func UpdateHabit(c *gin.Context) {
	// habitID := c.Param("habitid")
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.InternalErrorResponse(c, nil)
		return
	}

	// habit := database.UpdateHabit(c, habitID)
}

func DeleteHabit(c *gin.Context) {
	// habitID := c.Param("habitid")

}
