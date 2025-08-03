package types

import "time"

type User struct {
	UserID    string `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`
	FirstName string `gorm:"column:first_name" json:"first_name"`
	LastName  string `gorm:"column:last_name" json:"last_name"`
	Email     string `gorm:"column:email" json:"email"`
	CreatedAt time.Time
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


type Roadmap struct {
    RoadmapID  string      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"roadmap_id"`
    UserID     string      `gorm:"type:uuid" json:"user_id"`
    Goal       string      `json:"goal"`
    Description string     `json:"description"`
    Days       int         `json:"days"`
    Completed  bool        `json:"completed"`
    StartDate  time.Time   `json:"start_date,omitempty"`
    Milestones []Milestone `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"milestones"`
}

type Milestone struct {
    MilestoneID string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"milestone_id"`
    RoadmapID   string     `gorm:"type:uuid" json:"roadmap_id"`
    UserID     string      `gorm:"type:uuid" json:"user_id"`
    SubGoal     string    `json:"sub_goal"`
    Description string    `json:"description"`
    Day        int       `json:"day"`
    Reached     bool      `json:"reached"`
    Tasks       []Task    `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"tasks"`
}

type Task struct {
    TaskID      string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"task_id"`
    MilestoneID string     `gorm:"type:uuid" json:"milestone_id"`
    UserID     string      `gorm:"type:uuid" json:"user_id"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    Day         int      `json:"day"`
    Completed   bool     `json:"completed"`
    Steps       []Step   `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE;" json:"steps"`
}

type Step struct {
    StepID      string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"step_id"`
    TaskID      string     `gorm:"type:uuid" json:"task_id"`
    Description string   `json:"description"`
    Completed   bool     `json:"completed"`
    Number      int      `json:"number"`
}