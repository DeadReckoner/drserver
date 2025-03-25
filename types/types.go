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
	TaskID       uint   `gorm:"primaryKey;column:task_id" json:"task_id"`
	UserID       uint   `gorm:"index" json:"user_id"`
	Title        string `gorm:"column:title" json:"title"`
	Description  string `gorm:"column:description" json:"description"`
	Level        string `gorm:"column:level" json:"level"`
	Completed    bool   `gorm:"column:completed" json:"complete"`
	StartDate    string `gorm:"column:start_date" json:"start_date"`
	EndDate      string `gorm:"column:end_date" json:"end_date"`
	CreatedAt    time.Time
	ParentTaskID *uint `gorm:"index" json:"parent_task_id"`
}
