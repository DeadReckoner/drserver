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
	ThreadID        string    `json:"thread_id"`
}

type AnchorRequest struct {
	RequestID   string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:request_id" json:"request_id"`
	RoadmapID   string     `gorm:"type:uuid;column:roadmap_id;not null" json:"roadmap_id"`
	RequesterID string     `gorm:"type:uuid;column:requester_id;not null" json:"requester_id"`
	AnchorID    string     `gorm:"type:uuid;column:anchor_id;not null" json:"anchor_id"`
	Status      string     `gorm:"column:status;default:pending" json:"status"` // "pending", "accepted", "rejected"
	CreatedAt   time.Time  `gorm:"column:created_at;default:now()" json:"created_at"`
	UpdatedAt   *time.Time `gorm:"column:updated_at" json:"updated_at,omitempty"`

	// Relationships for preloading
	Roadmap   *Roadmap `gorm:"foreignKey:RoadmapID;references:RoadmapID" json:"roadmap,omitempty"`
	Requester *User    `gorm:"foreignKey:RequesterID;references:UserID" json:"requester,omitempty"`
	Anchor    *User    `gorm:"foreignKey:AnchorID;references:UserID" json:"anchor,omitempty"`
}

func (AnchorRequest) TableName() string {
	return "anchor_requests"
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
	AnchorID    *string   `gorm:"type:uuid;column:anchor_id" json:"anchor_id,omitempty"`

	// Async generation tracking
	JobID            *string `gorm:"type:uuid;column:job_id" json:"job_id,omitempty"`
	GenerationStatus string  `gorm:"type:generation_status;column:generation_status;default:'pending'" json:"generation_status"`

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

	// Resources forecast stored as JSON string (text)
	ResourcesForecast *string `gorm:"type:text" json:"resources_forecast,omitempty"`

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
	Dependencies *string `gorm:"type:text" json:"dependencies,omitempty"`
	Confidence   string  `json:"confidence,omitempty"`

	Day       int       `json:"day"`
	Completed bool      `json:"completed"`
	Date      time.Time `json:"date"`
	Steps     []Step    `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE;" json:"steps,omitempty"`
}

// =============================================================================
// HABIT SYSTEM TYPES
// =============================================================================

// RecurrenceType defines how a habit recurs
type RecurrenceType string

const (
	RecurrenceNone    RecurrenceType = "none"    // One-time todo
	RecurrenceDaily   RecurrenceType = "daily"   // Every day
	RecurrenceWeekly  RecurrenceType = "weekly"  // Specific days of week
	RecurrenceMonthly RecurrenceType = "monthly" // Specific day of month
)

// Validate checks if the recurrence type is valid
func (r RecurrenceType) Validate() bool {
	switch r {
	case RecurrenceNone, RecurrenceDaily, RecurrenceWeekly, RecurrenceMonthly:
		return true
	}
	return false
}

// DayOfWeek represents a bitmask for days of the week
type DayOfWeek int

const (
	Sunday    DayOfWeek = 1 << 0 // 1
	Monday    DayOfWeek = 1 << 1 // 2
	Tuesday   DayOfWeek = 1 << 2 // 4
	Wednesday DayOfWeek = 1 << 3 // 8
	Thursday  DayOfWeek = 1 << 4 // 16
	Friday    DayOfWeek = 1 << 5 // 32
	Saturday  DayOfWeek = 1 << 6 // 64
)

// Common day presets for convenience
const (
	Weekdays DayOfWeek = Monday | Tuesday | Wednesday | Thursday | Friday       // 62
	Weekends DayOfWeek = Saturday | Sunday                                       // 65
	EveryDay DayOfWeek = Sunday | Monday | Tuesday | Wednesday | Thursday | Friday | Saturday // 127
)

// DayOfWeekFromTime converts time.Weekday to our DayOfWeek bitmask value
func DayOfWeekFromTime(w time.Weekday) DayOfWeek {
	return 1 << DayOfWeek(w)
}

// Contains checks if a specific day is set in the bitmask
func (d DayOfWeek) Contains(day time.Weekday) bool {
	return d&DayOfWeekFromTime(day) != 0
}

// Days returns a slice of time.Weekday values that are set in the bitmask
func (d DayOfWeek) Days() []time.Weekday {
	var days []time.Weekday
	for i := time.Sunday; i <= time.Saturday; i++ {
		if d.Contains(i) {
			days = append(days, i)
		}
	}
	return days
}

// Habit represents a recurring or one-time todo/habit
type Habit struct {
	// Identity
	HabitID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"habit_id"`
	UserID  string `gorm:"type:uuid;index;not null" json:"user_id"`

	// Content
	Title       string `gorm:"not null" json:"title"`
	Description string `json:"description,omitempty"`

	// Scheduling - determines when this habit is due
	RecurrenceType RecurrenceType `gorm:"type:varchar(20);not null;default:'none'" json:"recurrence_type"`
	DaysOfWeek     int            `gorm:"default:0" json:"days_of_week,omitempty"`  // Bitmask for weekly recurrence
	DayOfMonth     *int           `json:"day_of_month,omitempty"`                   // 1-31 for monthly recurrence
	DueDate        *time.Time     `json:"due_date,omitempty"`                       // For one-time todos

	// Active period - when this habit is active
	StartDate time.Time  `gorm:"not null" json:"start_date"`
	EndDate   *time.Time `json:"end_date,omitempty"`

	// Status
	Archived bool `gorm:"default:false" json:"archived"`

	// Metadata
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	// Relationships (optional - for eager loading)
	Completions []HabitCompletion `gorm:"foreignKey:HabitID" json:"completions,omitempty"`
}

// TableName specifies the table name for GORM
func (Habit) TableName() string {
	return "habits"
}

// IsDueOn checks if the habit is scheduled for a given date
func (h *Habit) IsDueOn(date time.Time) bool {
	// Normalize input date and habit dates to start of day in UTC for comparison
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	startDateOnly := time.Date(h.StartDate.Year(), h.StartDate.Month(), h.StartDate.Day(), 0, 0, 0, 0, time.UTC)
	
	// Check if date is within active period
	if dateOnly.Before(startDateOnly) {
		return false
	}
	if h.EndDate != nil {
		endDateOnly := time.Date(h.EndDate.Year(), h.EndDate.Month(), h.EndDate.Day(), 0, 0, 0, 0, time.UTC)
		if dateOnly.After(endDateOnly) {
			return false
		}
	}

	switch h.RecurrenceType {
	case RecurrenceNone:
		// One-time todos should appear from start_date until due_date
		// This allows users to see upcoming tasks before they're due
		if h.DueDate == nil {
			return false
		}
		// Normalize due date to start of day for comparison
		dueDay := time.Date(h.DueDate.Year(), h.DueDate.Month(), h.DueDate.Day(), 0, 0, 0, 0, time.UTC)
		return !dateOnly.After(dueDay)
	case RecurrenceDaily:
		return true
	case RecurrenceWeekly:
		return DayOfWeek(h.DaysOfWeek).Contains(date.Weekday())
	case RecurrenceMonthly:
		return h.DayOfMonth != nil && date.Day() == *h.DayOfMonth
	}
	return false
}

// IsActive checks if the habit is currently active (not archived, within date range)
func (h *Habit) IsActive(date time.Time) bool {
	if h.Archived {
		return false
	}
	if date.Before(h.StartDate) {
		return false
	}
	if h.EndDate != nil && date.After(*h.EndDate) {
		return false
	}
	return true
}

// sameDay checks if two times represent the same calendar day
func sameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// HabitCompletion tracks when a habit was completed
type HabitCompletion struct {
	CompletionID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"completion_id"`
	HabitID      string `gorm:"type:uuid;index;not null" json:"habit_id"`
	UserID       string `gorm:"type:uuid;index;not null" json:"user_id"` // Denormalized for easier queries

	CompletedAt time.Time `gorm:"not null" json:"completed_at"`   // When they marked it complete
	Date        time.Time `gorm:"type:date;not null" json:"date"` // The date this completion is "for"
	Notes       string    `json:"notes,omitempty"`                // Optional notes

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName specifies the table name for GORM
func (HabitCompletion) TableName() string {
	return "habit_completions"
}

// =============================================================================
// HABIT REQUEST/RESPONSE TYPES
// =============================================================================

// CreateHabitRequest is the payload for creating a new habit
type CreateHabitRequest struct {
	Title          string         `json:"title" binding:"required"`
	Description    string         `json:"description"`
	RecurrenceType RecurrenceType `json:"recurrence_type" binding:"required"`
	DaysOfWeek     int            `json:"days_of_week"`
	DayOfMonth     *int           `json:"day_of_month"`
	DueDate        *time.Time     `json:"due_date"`
	StartDate      *time.Time     `json:"start_date"` // Defaults to today if not provided
	EndDate        *time.Time     `json:"end_date"`
}

// Validate checks if the CreateHabitRequest is valid
func (r *CreateHabitRequest) Validate() error {
	if !r.RecurrenceType.Validate() {
		return ErrInvalidRecurrenceType
	}

	switch r.RecurrenceType {
	case RecurrenceNone:
		if r.DueDate == nil {
			return ErrDueDateRequired
		}
	case RecurrenceWeekly:
		if r.DaysOfWeek == 0 || r.DaysOfWeek > 127 {
			return ErrInvalidDaysOfWeek
		}
	case RecurrenceMonthly:
		if r.DayOfMonth == nil || *r.DayOfMonth < 1 || *r.DayOfMonth > 31 {
			return ErrInvalidDayOfMonth
		}
	}

	return nil
}

// UpdateHabitRequest is the payload for updating a habit (all fields optional)
type UpdateHabitRequest struct {
	Title          *string         `json:"title,omitempty"`
	Description    *string         `json:"description,omitempty"`
	RecurrenceType *RecurrenceType `json:"recurrence_type,omitempty"`
	DaysOfWeek     *int            `json:"days_of_week,omitempty"`
	DayOfMonth     *int            `json:"day_of_month,omitempty"`
	DueDate        *time.Time      `json:"due_date,omitempty"`
	StartDate      *time.Time      `json:"start_date,omitempty"`
	EndDate        *time.Time      `json:"end_date,omitempty"`
	Archived       *bool           `json:"archived,omitempty"`
}

// CompleteHabitRequest is the payload for marking a habit complete
type CompleteHabitRequest struct {
	Date  *time.Time `json:"date"`  // Defaults to today if not provided
	Notes string     `json:"notes"`
}

// HabitWithStatus represents a habit with its completion status for a specific date
type HabitWithStatus struct {
	Habit
	IsCompleted bool       `json:"is_completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// HabitStatsResponse contains statistics for a habit
type HabitStatsResponse struct {
	CurrentStreak      int     `json:"current_streak"`
	CompletionRate7d   float64 `json:"completion_rate_7d"`
	CompletionRate30d  float64 `json:"completion_rate_30d"`
	TotalCompletions   int     `json:"total_completions"`
}

// =============================================================================
// HABIT ERRORS
// =============================================================================

// HabitError represents a habit-specific error
type HabitError struct {
	Message string
}

func (e HabitError) Error() string {
	return e.Message
}

var (
	ErrInvalidRecurrenceType = HabitError{"invalid recurrence type"}
	ErrDueDateRequired       = HabitError{"due_date is required for non-recurring habits"}
	ErrInvalidDaysOfWeek     = HabitError{"days_of_week must be between 1 and 127"}
	ErrInvalidDayOfMonth     = HabitError{"day_of_month must be between 1 and 31"}
	ErrHabitNotFound         = HabitError{"habit not found"}
	ErrAlreadyCompleted      = HabitError{"habit already completed for this date"}
)

type Step struct {
	StepID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"step_id"`
	TaskID      string `gorm:"type:uuid" json:"task_id"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
	Number      int    `json:"number"`
}

type Thread struct {
	ThreadID        string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"thread_id"`
	Name            string    `json:"name"`
	TotalUnread     int       `json:"total_unread"`
	LastSentMessage *string   `gorm:"type:uuid" json:"last_sent_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Messages struct {
	MessageID string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"message_id"`
	ThreadID  string     `gorm:"type:uuid" json:"thread_id"`
	UserID    string     `gorm:"type:uuid" json:"user_id"`
	Content   string     `json:"content"`
	Read      bool       `json:"read"`
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
	ThreadID        string    `json:"thread_id"`
	Name            string    `json:"name"`
	Participants    []User    `json:"participants"`
	TotalUnread     int       `json:"total_unread"`
	LastSentMessage string    `json:"last_sent_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastMessage     *MessageResponse
}

type MessageResponse struct {
	MessageID string    `json:"message_id"`
	ThreadID  string    `json:"thread_id"`
	UserID    string    `json:"user_id"`
	Content   string    `json:"content"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"created_at"`
	User      *User     `json:"user,omitempty"`
}

type BackgroundJob struct {
	JobID       string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:job_id" json:"job_id"`
	UserID      string     `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	Type        string     `gorm:"column:type;not null" json:"type"`
	Status      string     `gorm:"column:status;default:pending" json:"status"`
	Progress    int        `gorm:"column:progress;default:0" json:"progress"`
	Payload     any        `gorm:"type:jsonb;column:payload;serializer:json" json:"payload"`
	Result      any        `gorm:"type:jsonb;column:result;serializer:json" json:"result,omitempty"`
	Error       string     `gorm:"column:error" json:"error,omitempty"`
	StartedAt   *time.Time `gorm:"column:started_at" json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
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
	Data           map[string]any `gorm:"type:jsonb;column:data;serializer:json" json:"data,omitempty"`
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
	NotificationStatusPending NotificationStatus = "pending"
	NotificationStatusSent    NotificationStatus = "sent"
	NotificationStatusFailed  NotificationStatus = "failed"
	NotificationStatusRead    NotificationStatus = "read"
)

type GenerationStatus string

const (
	GenerationStatusPending    GenerationStatus = "pending"
	GenerationStatusProcessing GenerationStatus = "processing"
	GenerationStatusCompleted  GenerationStatus = "completed"
	GenerationStatusFailed     GenerationStatus = "failed"
)
