package types

import (
	"time"
)

type User struct {
	UserID      string `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`
	FirstName   string `gorm:"column:first_name" json:"first_name"`
	LastName    string `gorm:"column:last_name" json:"last_name"`
	Email       string `gorm:"column:email" json:"email"`
	PhoneNumber string `gorm:"column:phone_number" json:"phone_number"`
	CreatedAt   time.Time
}

type Friendship struct {
	ID           string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:id" json:"id"`
	RequesterID  string         `gorm:"type:uuid;column:requester_id;not null" json:"requester_id"`
	AddresseeID  string         `gorm:"type:uuid;column:addressee_id;not null" json:"addressee_id"`
	Status       string         `gorm:"column:status;default:pending" json:"status"`
	Restrictions map[string]any `gorm:"type:jsonb;column:restrictions" json:"restrictions,omitempty"`
	CreatedAt    time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    *time.Time     `gorm:"column:updated_at" json:"updated_at,omitempty"`

	// Relationships for preloading
	Requester *User `gorm:"foreignKey:RequesterID;references:UserID" json:"requester,omitempty"`
	Addressee *User `gorm:"foreignKey:AddresseeID;references:UserID" json:"addressee,omitempty"`
}

func (Friendship) TableName() string {
	return "friendships"
}

type UserSearchResult struct {
	UserID           string `json:"user_id"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	Email            string `json:"email"`
	FriendshipStatus string `json:"friendship_status"` // "accepted", "pending", or "none"
}

type UpdateFriendshipStatusRequest struct {
	Status string `json:"status" binding:"required"` // "accepted" or "rejected"
}

type FriendResponse struct {
	FriendshipID string         `json:"friendship_id"`
	Friend       User           `json:"friend"`
	Restrictions map[string]any `json:"restrictions,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type FriendProfileResponse struct {
	User            User      `json:"user"`
	AllowedRoadmaps []Roadmap `json:"allowed_roadmaps"`
}

type Prompt struct {
	UserPrompt string `json:"user_prompt"`
}

type GoalRequest struct {
	GoalName             string `json:"goalName"`
	GoalDescription      string `json:"goalDescription"`
	GoalImportance       string `json:"goalImportance"`
	TargetCompletionDate string `json:"targetCompletionDate"`
	WeeklyTimeCommitment string `json:"weeklyTimeCommitment"`
	WorkFrequency        string `json:"workFrequency"`
	Constraints          string `json:"constraints"`
	CurrentSkillLevel    string `json:"currentSkillLevel"`
}

type OpenAIContent struct {
	Type        string `json:"type"`
	Annotations []any  `json:"annotations"`
	Text        string `json:"text"` // this is a JSON string
}

type OpenAIMessage struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Status  string          `json:"status"`
	Content []OpenAIContent `json:"content"`
}

type OpenAIResponse struct {
	ID     string          `json:"id"`
	Output []OpenAIMessage `json:"output"`
}

type Roadmap struct {
	RoadmapID   string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"roadmap_id"`
	UserID      string    `gorm:"type:uuid" json:"user_id"`
	Goal        string    `json:"goal"`
	Description string    `json:"description"`
	Days        int       `json:"days"`
	Completed   bool      `json:"completed"`
	StartDate   time.Time `json:"start_date,omitempty"`
	Type        string    `gorm:"type:roadmap_type;default:'custom_made'" json:"type"` // "ai_generated" or "custom_made"

	// Link to Macro Map (strategic overview)
	MacroMap *MacroMap `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"macro_map,omitempty"`

	// Milestones represent the detailed tactical plan (active milestone)
	Milestones []Milestone `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"milestones,omitempty"`
}

// MacroMap represents the strategic overview of a roadmap
type MacroMap struct {
	MacroMapID string  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"macro_map_id"`
	RoadmapID  string  `gorm:"type:uuid;unique" json:"roadmap_id"`
	Phases     []Phase `gorm:"foreignKey:MacroMapID;constraint:OnDelete:CASCADE;" json:"phases,omitempty"`
}

// Phase represents a high-level stage in the macro map
type Phase struct {
	PhaseID         string           `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"phase_id"`
	MacroMapID      string           `gorm:"type:uuid" json:"macro_map_id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	OrderIndex      int              `json:"order_index"`
	DurationWeeks   string           `json:"duration_weeks"`
	MacroMilestones []MacroMilestone `gorm:"foreignKey:PhaseID;constraint:OnDelete:CASCADE;" json:"macro_milestones,omitempty"`
}

// MacroMilestone represents a strategic milestone within a phase
type MacroMilestone struct {
	MacroMilestoneID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"macro_milestone_id"`
	PhaseID          string `gorm:"type:uuid" json:"phase_id"`
	Name             string `json:"name"`
	AcceptanceTest   string `json:"acceptance_test"`
	DurationDaysMin  int    `json:"duration_days_min"`
	DurationDaysMax  int    `json:"duration_days_max"`
	OrderIndex       int    `json:"order_index"`

	// Resources forecast stored as JSONB
	ResourcesForecast *string `gorm:"type:jsonb" json:"resources_forecast,omitempty"`

	// Related spikes and risks
	Spikes []Spike `gorm:"foreignKey:MacroMilestoneID;constraint:OnDelete:CASCADE;" json:"spikes,omitempty"`
	Risks  []Risk  `gorm:"foreignKey:MacroMilestoneID;constraint:OnDelete:CASCADE;" json:"risks,omitempty"`

	// Link to detailed Milestone Map (when generated)
	DetailedMilestoneID *string `gorm:"type:uuid" json:"detailed_milestone_id,omitempty"`
}

// Spike represents a time-boxed investigation or experiment
type Spike struct {
	SpikeID          string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"spike_id"`
	MacroMilestoneID string `gorm:"type:uuid" json:"macro_milestone_id"`
	Description      string `json:"description"`
	TimeboxDays      int    `json:"timebox_days"`
}

// Risk represents a potential issue or blocker
type Risk struct {
	RiskID           string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"risk_id"`
	MacroMilestoneID string `gorm:"type:uuid" json:"macro_milestone_id"`
	Description      string `json:"description"`
	Severity         string `json:"severity"`
}

type Milestone struct {
	MilestoneID string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"milestone_id"`
	RoadmapID   string    `gorm:"type:uuid" json:"roadmap_id"`
	UserID      string    `gorm:"type:uuid" json:"user_id"`
	SubGoal     string    `json:"sub_goal"`
	Description string    `json:"description"`
	Day         int       `json:"day"`
	Reached     bool      `json:"reached"`
	Date        time.Time `json:"date,omitempty"`

	// Link back to MacroMilestone (if this is a detailed plan from a macro milestone)
	MacroMilestoneID *string `gorm:"type:uuid" json:"macro_milestone_id,omitempty"`

	Tasks []Task `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"tasks,omitempty"`
}

type Task struct {
	TaskID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"task_id"`
	RoadmapID   string `gorm:"type:uuid" json:"roadmap_id"`
	MilestoneID string `gorm:"type:uuid" json:"milestone_id"`
	UserID      string `gorm:"type:uuid" json:"user_id"`
	Title       string `json:"title"`
	Description string `json:"description"`

	// New fields from Milestone Map spec
	Rationale    string  `json:"rationale,omitempty"`
	EstMinutes   int     `json:"est_minutes,omitempty"`
	Dependencies *string `gorm:"type:jsonb" json:"dependencies,omitempty"`
	Confidence   string  `json:"confidence,omitempty"`

	Day       int       `json:"day"`
	Completed bool      `json:"completed"`
	Date      time.Time `json:"date"`
	Steps     []Step    `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE;" json:"steps,omitempty"`
}

type Step struct {
	StepID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"step_id"`
	TaskID      string `gorm:"type:uuid" json:"task_id"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
	Number      int    `json:"number"`
}

type Thread struct {
	ThreadID  string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"thread_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Messages struct {
	MessageID string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"message_id"`
	ThreadID  string     `gorm:"type:uuid" json:"thread_id"`
	UserID    string     `gorm:"type:uuid" json:"user_id"`
	Content   string     `json:"content"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type ThreadParticipants struct {
	ThreadPID string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"thread_p_id"`
	ThreadID  string     `gorm:"type:uuid" json:"thread_id"`
	UserID    string     `gorm:"type:uuid" json:"user_id"`
	JoinedAt  time.Time  `json:"joined_at"`
	LeftAt    *time.Time `json:"left_at,omitempty"`
	Role      string     `json:"role"`
}

// Response types for messaging API

type ThreadWithParticipants struct {
	ThreadID     string    `json:"thread_id"`
	Name         string    `json:"name"`
	Participants []User    `json:"participants"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type MessageResponse struct {
	MessageID string    `json:"message_id"`
	ThreadID  string    `json:"thread_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	User      *User     `json:"user,omitempty"`
}

type BackgroundJob struct {
	JobID      string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:job_id" json:"job_id"`
	UserID     string     `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	Type       string     `gorm:"column:type;not null" json:"type"`
	Status     string     `gorm:"column:status;default:pending" json:"status"`
	Progress   int        `gorm:"column:progress;default:0" json:"progress"`
	Payload    any        `gorm:"type:jsonb;column:payload" json:"payload"`
	Result     any        `gorm:"type:jsonb;column:result" json:"result,omitempty"`
	Error      string     `gorm:"column:error" json:"error,omitempty"`
	StartedAt  *time.Time `gorm:"column:started_at" json:"started_at,omitempty"`
	CompleteAt *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
	//why not pointer?
	CreatedAt time.Time `gorm:"column:created_at;default:now()" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;default:now()" json:"updated_at"`
}

// What is the purpose of this ?
func (BackgroundJob) TableName() string {
	return "background_jobs"
}

// PushToken represents an Expo push notification token
type PushToken struct {
	TokenID    string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:token_id" json:"token_id"`
	UserID     string    `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	ExpoToken  string    `gorm:"column:expo_token;not null;unique" json:"expo_token"`
	Platform   string    `gorm:"column:platform" json:"platform"` // 'ios' or 'android'
	DeviceID   string    `gorm:"column:device_id" json:"device_id,omitempty"`
	IsActive   bool      `gorm:"column:is_active;default:true" json:"is_active"`
	LastUsedAt time.Time `gorm:"column:last_used_at;default:now()" json:"last_used_at"`
	CreatedAt  time.Time `gorm:"column:created_at;default:now()" json:"created_at"`
}

func (PushToken) TableName() string {
	return "push_tokens"
}

// Notification represents a push notification record
type Notification struct {
	NotificationID string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:notification_id" json:"notification_id"`
	UserID         string         `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	Type           string         `gorm:"column:type;not null" json:"type"`
	Title          string         `gorm:"column:title;not null" json:"title"`
	Body           string         `gorm:"column:body;not null" json:"body"`
	Data           map[string]any `gorm:"type:jsonb;column:data" json:"data,omitempty"`
	Status         string         `gorm:"column:status;default:pending" json:"status"` // 'pending', 'sent', 'failed', 'read'
	SentAt         *time.Time     `gorm:"column:sent_at" json:"sent_at,omitempty"`
	ReadAt         *time.Time     `gorm:"column:read_at" json:"read_at,omitempty"`
	Error          string         `gorm:"column:error" json:"error,omitempty"`
	CreatedAt      time.Time      `gorm:"column:created_at;default:now()" json:"created_at"`
}

func (Notification) TableName() string {
	return "notifications"
}

// ProgressUpdate represents a WebSocket message for generation progress
type ProgressUpdate struct {
	Type     string `json:"type"` // 'progress', 'status', 'complete', 'error'
	JobID    string `json:"job_id"`
	Progress int    `json:"progress"`       // 0-100
	Status   string `json:"status"`         // 'pending', 'processing', 'completed', 'failed'
	Message  string `json:"message"`        // Human-readable status
	Data     any    `json:"data,omitempty"` // Additional context (roadmap_id, etc.)
}

type RoadmapType string

const (
	RoadmapTypeAIGenerated RoadmapType = "ai_generated"
	RoadmapTypeCustomMade  RoadmapType = "custom_made"
)

type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusProcessing JobStatus = "processing"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

type JobType string

const (
	JobTypeRoadmap   JobType = "roadmap_generation"
	JobTypeMilestone JobType = "milestone_generation"
)

type PlatformType string

const (
	PlatformTypeIOS     PlatformType = "ios"
	PlatformTypeAndroid PlatformType = "android"
)

type NotificationStatus string

const (
	NotificaitonStatusPending NotificationStatus = "pending"
	NotificaitonStatusSent    NotificationStatus = "sent"
	NotificaitonStatusFailed  NotificationStatus = "failed"
	NotificaitonStatusRead    NotificationStatus = "read"
)

type GenerationStatus string

const (
	GenerationStatusPending    GenerationStatus = "pending"
	GenerationStatusProcessing GenerationStatus = "processing"
	GenerationStatusCompleted  GenerationStatus = "completed"
	GenerationStatusFailed     GenerationStatus = "failed"
)
