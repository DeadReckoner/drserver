package types

import "time"

type User struct {
	UserID    string `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`
	FirstName string `gorm:"column:first_name" json:"first_name"`
	LastName  string `gorm:"column:last_name" json:"last_name"`
	Email     string `gorm:"column:email" json:"email"`
	CreatedAt time.Time
}

type Task struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:level`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Completed   bool   `json:"completed"`
}
