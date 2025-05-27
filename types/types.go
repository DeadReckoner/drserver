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

type Prompt struct{
	UserPrompt string `json:"user_prompt"`
}

type OpenAIContent struct {
	Type        string `json:"type"`
	Annotations []any  `json:"annotations"`
	Text        string `json:"text"` // this is a JSON string
}

type OpenAIMessage struct {
	ID     string           `json:"id"`
	Type   string           `json:"type"`
	Status string           `json:"status"`
	Content []OpenAIContent `json:"content"`
}

type OpenAIResponse struct {
	ID     string           `json:"id"`
	Output []OpenAIMessage  `json:"output"`
}


type GoalRoadmap struct {
	Message     string        `json:"message"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Level       string        `json:"level"`
	StartTime   string        `json:"start_time"`
	EndTime     string        `json:"end_time"`
	Completed   bool          `json:"completed"`
	Subtasks    []GoalRoadmap `json:"subtasks"`
}
