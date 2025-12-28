package database

import (
	"dr/server/types"
	"errors"

	"github.com/gin-gonic/gin"
)

func GetHabit(c *gin.Context, habitID string) types.Habit {

	var habit types.Habit

	return habit
}

func CreateHabit(c *gin.Context, habit types.Habit) (types.Habit, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Habit{}, errors.New("missing or invalid user_id")
	}

	habit.UserID = user_id.(string)

	tx := db.Create(&habit)
	if tx.Error != nil {
		return types.Habit{}, tx.Error
	}

	var createdhabit types.Habit
	if err := db.Where("habit_id = ?", habit.HabitID).Preload("Steps").First(&createdhabit).Error; err != nil {
		return types.Habit{}, err
	}

	return createdhabit, nil

}
