package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"net/http"

	"github.com/gin-gonic/gin"
)




func GetHabit(c *gin.Context){
	habitID := c.Param("habitid")
	habit := database.GetHabit(c, habitID)

	if habit.HabitID == "" {
		utils.NotFoundResponse(c, "Habit")
		return
	}

	utils.DataResponse(c, http.StatusOK, habit)
}