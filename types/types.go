package types

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

/******************************************************************************
 * SHARED ENUMS & CONSTANTS
 ******************************************************************************/

type Status string

const (
	StatusNotStarted Status = "not_started"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusOverdue    Status = "overdue"
)

func (s Status) Validate() bool {
	switch s {
	case StatusNotStarted, StatusInProgress, StatusCompleted, StatusOverdue:
		return true
	}
	return false
}

type RoadmapType string

const (
	RoadmapTypeAIGenerated RoadmapType = "ai_generated"
	RoadmapTypeCustomMade  RoadmapType = "custom_made"
)

type GenerationStatus string

const (
	GenerationStatusPending    GenerationStatus = "pending"
	GenerationStatusProcessing GenerationStatus = "processing"
	GenerationStatusCompleted  GenerationStatus = "completed"
	GenerationStatusFailed     GenerationStatus = "failed"
)

/******************************************************************************
 * USER
 ******************************************************************************/

type User struct {
	UserID      string `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`
	FirstName   string `gorm:"column:first_name" json:"first_name,omitempty"`
	LastName    string `gorm:"column:last_name" json:"last_name,omitempty"`
	Email       string `gorm:"column:email" json:"email"`
	PhoneNumber string `gorm:"column:phone_number" json:"phone_number"`
	CreatedAt   time.Time

	// FullName is used for Apple/Google sign-in which returns full_name instead of first_name/last_name.
	// This field is not stored in the database - it's parsed into FirstName/LastName on user creation.
	FullName string `gorm:"-" json:"full_name,omitempty"`
}

// ParseFullName splits FullName into FirstName and LastName if they are not already set.
func (u *User) ParseFullName() {
	if u.FullName == "" || (u.FirstName != "" && u.LastName != "") {
		return
	}

	fullName := strings.TrimSpace(u.FullName)
	if fullName == "" {
		return
	}

	parts := strings.SplitN(fullName, " ", 2)

	if len(parts) >= 1 {
		u.FirstName = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		u.LastName = strings.TrimSpace(parts[1])
	}
}

/******************************************************************************
 * FRIENDSHIPS & SOCIAL
 ******************************************************************************/

type Friendship struct {
	ID           string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:id" json:"id"`
	RequesterID  string         `gorm:"type:uuid;column:requester_id;not null" json:"requester_id"`
	AddresseeID  string         `gorm:"type:uuid;column:addressee_id;not null" json:"addressee_id"`
	Status       string         `gorm:"column:status;default:pending" json:"status"`
	Restrictions map[string]any `gorm:"type:jsonb;column:restrictions" json:"restrictions,omitempty"`
	CreatedAt    time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt    *time.Time     `gorm:"column:updated_at" json:"updated_at,omitempty"`

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

/******************************************************************************
 * ANCHOR REQUESTS
 ******************************************************************************/

const (
	TargetTypeRoadmap = "roadmap"
	TargetTypeHabit   = "habit"
)

type AnchorRequest struct {
	RequestID   string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:request_id" json:"request_id"`
	TargetType  string     `gorm:"column:target_type;default:roadmap" json:"target_type"` // "roadmap" or "habit"
	RoadmapID   *string    `gorm:"type:uuid;column:roadmap_id" json:"roadmap_id,omitempty"`
	HabitID     *string    `gorm:"type:uuid;column:habit_id" json:"habit_id,omitempty"`
	RequesterID string     `gorm:"type:uuid;column:requester_id;not null" json:"requester_id"`
	AnchorID    string     `gorm:"type:uuid;column:anchor_id;not null" json:"anchor_id"`
	Status      string     `gorm:"column:status;default:pending" json:"status"` // "pending", "accepted", "rejected"
	CreatedAt   time.Time  `gorm:"column:created_at;default:now()" json:"created_at"`
	UpdatedAt   *time.Time `gorm:"column:updated_at" json:"updated_at,omitempty"`

	Roadmap   *Roadmap `gorm:"foreignKey:RoadmapID;references:RoadmapID" json:"roadmap,omitempty"`
	Habit     *Habit   `gorm:"foreignKey:HabitID;references:HabitID" json:"habit,omitempty"`
	Requester *User    `gorm:"foreignKey:RequesterID;references:UserID" json:"requester,omitempty"`
	Anchor    *User    `gorm:"foreignKey:AnchorID;references:UserID" json:"anchor,omitempty"`
}

func (AnchorRequest) TableName() string {
	return "anchor_requests"
}

/******************************************************************************
 * ROADMAP HIERARCHY: Roadmap -> Phase -> Milestone -> Task -> Step
 ******************************************************************************/

type Roadmap struct {
	RoadmapID         string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"roadmap_id"`
	UserID            string     `gorm:"type:uuid" json:"user_id"`
	Goal              string     `json:"goal"`
	Description       string     `json:"description"`
	Days              int        `json:"days"`
	Completed         bool       `json:"completed"`
	StartDate         *time.Time `json:"start_date"`
	Type              string     `gorm:"type:roadmap_type;default:'custom_made'" json:"type"` // "ai_generated" or "custom_made"
	AnchorID          *string    `gorm:"type:uuid;column:anchor_id" json:"anchor_id,omitempty"`
	CurrentSkillLevel string     `gorm:"column:current_skill_level" json:"current_skill_level,omitempty"`

	// Async generation tracking
	JobID            *string `gorm:"type:uuid;column:job_id" json:"job_id,omitempty"`
	GenerationStatus string  `gorm:"type:generation_status;column:generation_status;default:'pending'" json:"generation_status"`

	// Relationships
	Phases     []Phase     `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"phases,omitempty"`
	Milestones []Milestone `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"milestones,omitempty"`
}

type Phase struct {
	PhaseID       string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"phase_id"`
	RoadmapID     string `gorm:"type:uuid" json:"roadmap_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	OrderIndex    int    `json:"order_index"`
	DurationWeeks string `json:"duration_weeks"`
	Status        string `json:"status"`

	Milestones []Milestone `gorm:"foreignKey:PhaseID;constraint:OnDelete:SET NULL;" json:"milestones,omitempty"`
}

// BeforeSave is a GORM hook that sets a default status for Phase
func (p *Phase) BeforeSave(tx *gorm.DB) error {
	if p.Status == "" {
		p.Status = string(StatusNotStarted)
	}
	return nil
}

type Milestone struct {
	MilestoneID string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"milestone_id"`
	RoadmapID   string     `gorm:"type:uuid" json:"roadmap_id"`
	UserID      string     `gorm:"type:uuid" json:"user_id"`
	SubGoal     string     `json:"sub_goal"`
	Description string     `json:"description"`
	Day         int        `json:"day"`
	Reached     bool       `json:"reached"`
	Date        *time.Time `json:"date"`
	Status      string     `json:"status"`

	// Strategic fields
	PhaseID           *string `gorm:"type:uuid" json:"phase_id,omitempty"`
	AcceptanceTest    string  `json:"acceptance_test,omitempty"`
	DurationDaysMin   int     `json:"duration_days_min,omitempty"`
	DurationDaysMax   int     `json:"duration_days_max,omitempty"`
	OrderIndex        int     `json:"order_index"`
	ResourcesForecast *string `gorm:"type:text" json:"resources_forecast,omitempty"`

	// Relationships
	Spikes []Spike `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"spikes,omitempty"`
	Risks  []Risk  `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"risks,omitempty"`
	Tasks  []Task  `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"tasks,omitempty"`
}

// ComputeStatus calculates the status based on Reached and Date fields
func (m *Milestone) ComputeStatus() string {
	if m.Reached {
		return string(StatusCompleted)
	}
	if m.Date == nil {
		return string(StatusNotStarted)
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	milestoneDate := time.Date(m.Date.Year(), m.Date.Month(), m.Date.Day(), 0, 0, 0, 0, m.Date.Location())

	if milestoneDate.After(today) {
		return string(StatusNotStarted)
	}
	if milestoneDate.Equal(today) {
		return string(StatusInProgress)
	}
	return string(StatusOverdue)
}

// BeforeSave is a GORM hook that updates Status before saving
func (m *Milestone) BeforeSave(tx *gorm.DB) error {
	m.Status = m.ComputeStatus()
	return nil
}

type Spike struct {
	SpikeID     string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"spike_id"`
	MilestoneID string `gorm:"type:uuid" json:"milestone_id"`
	Description string `json:"description"`
	TimeboxDays int    `json:"timebox_days"`
}

type Risk struct {
	RiskID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"risk_id"`
	MilestoneID string `gorm:"type:uuid" json:"milestone_id"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
}

type Task struct {
	TaskID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"task_id"`
	RoadmapID   string `gorm:"type:uuid" json:"roadmap_id"`
	MilestoneID string `gorm:"type:uuid" json:"milestone_id"`
	UserID      string `gorm:"type:uuid" json:"user_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`

	// Planning fields
	Rationale    string  `json:"rationale,omitempty"`
	EstMinutes   int     `json:"est_minutes,omitempty"`
	Dependencies *string `gorm:"type:text" json:"dependencies,omitempty"`
	Confidence   string  `json:"confidence,omitempty"`

	Day       int        `json:"day"`
	Completed bool       `json:"completed"`
	Date      *time.Time `json:"date"`

	Steps []Step `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE;" json:"steps,omitempty"`
}

// ComputeStatus calculates the status based on Completed and Date fields
func (t *Task) ComputeStatus() string {
	if t.Completed {
		return string(StatusCompleted)
	}
	if t.Date == nil {
		return string(StatusNotStarted)
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	taskDate := time.Date(t.Date.Year(), t.Date.Month(), t.Date.Day(), 0, 0, 0, 0, t.Date.Location())

	if taskDate.After(today) {
		return string(StatusNotStarted)
	}
	if taskDate.Equal(today) {
		return string(StatusInProgress)
	}
	return string(StatusOverdue)
}

// BeforeSave is a GORM hook that updates Status before saving
func (t *Task) BeforeSave(tx *gorm.DB) error {
	t.Status = t.ComputeStatus()
	return nil
}

type Step struct {
	StepID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"step_id"`
	TaskID      string `gorm:"type:uuid" json:"task_id"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
	Number      int    `json:"number"`
}

/******************************************************************************
 * AI AGENT
 ******************************************************************************/

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
	AdditionalContext    string `json:"additionalContext"`
}

type OpenAIContent struct {
	Type        string `json:"type"`
	Annotations []any  `json:"annotations"`
	Text        string `json:"text"`
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

/******************************************************************************
 * HABITS
 ******************************************************************************/

type RecurrenceType string

const (
	RecurrenceNone    RecurrenceType = "none"    // One-time todo
	RecurrenceDaily   RecurrenceType = "daily"   // Every day
	RecurrenceWeekly  RecurrenceType = "weekly"  // Specific days of week
	RecurrenceMonthly RecurrenceType = "monthly" // Specific day of month
)

func (r RecurrenceType) Validate() bool {
	switch r {
	case RecurrenceNone, RecurrenceDaily, RecurrenceWeekly, RecurrenceMonthly:
		return true
	}
	return false
}

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

const (
	Weekdays DayOfWeek = Monday | Tuesday | Wednesday | Thursday | Friday                     // 62
	Weekends DayOfWeek = Saturday | Sunday                                                    // 65
	EveryDay DayOfWeek = Sunday | Monday | Tuesday | Wednesday | Thursday | Friday | Saturday // 127
)

func DayOfWeekFromTime(w time.Weekday) DayOfWeek {
	return 1 << DayOfWeek(w)
}

func (d DayOfWeek) Contains(day time.Weekday) bool {
	return d&DayOfWeekFromTime(day) != 0
}

func (d DayOfWeek) Days() []time.Weekday {
	var days []time.Weekday
	for i := time.Sunday; i <= time.Saturday; i++ {
		if d.Contains(i) {
			days = append(days, i)
		}
	}
	return days
}

type Habit struct {
	HabitID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"habit_id"`
	UserID  string `gorm:"type:uuid;index;not null" json:"user_id"`

	Title       string `gorm:"not null" json:"title"`
	Description string `json:"description,omitempty"`

	// Scheduling
	RecurrenceType RecurrenceType `gorm:"type:varchar(20);not null;default:'none'" json:"recurrence_type"`
	DaysOfWeek     int            `gorm:"default:0" json:"days_of_week,omitempty"`
	DayOfMonth     *int           `json:"day_of_month,omitempty"`
	DueDate        *time.Time     `json:"due_date,omitempty"`

	// Active period
	StartDate time.Time  `gorm:"not null" json:"start_date"`
	EndDate   *time.Time `json:"end_date,omitempty"`

	Archived bool `gorm:"default:false" json:"archived"`

	// Anchor
	AnchorID *string `gorm:"type:uuid;column:anchor_id" json:"anchor_id,omitempty"`
	Anchor   *User   `gorm:"foreignKey:AnchorID;references:UserID" json:"anchor,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	Completions []HabitCompletion `gorm:"foreignKey:HabitID" json:"completions,omitempty"`
}

func (Habit) TableName() string {
	return "habits"
}

func (h *Habit) IsDueOn(date time.Time) bool {
	dateOnly := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	startDateOnly := time.Date(h.StartDate.Year(), h.StartDate.Month(), h.StartDate.Day(), 0, 0, 0, 0, time.UTC)

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
		if h.DueDate == nil {
			return false
		}
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

type HabitCompletion struct {
	CompletionID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"completion_id"`
	HabitID      string `gorm:"type:uuid;index;not null" json:"habit_id"`
	UserID       string `gorm:"type:uuid;index;not null" json:"user_id"`

	CompletedAt time.Time `gorm:"not null" json:"completed_at"`
	Date        time.Time `gorm:"type:date;not null" json:"date"`
	Notes       string    `json:"notes,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (HabitCompletion) TableName() string {
	return "habit_completions"
}

// Habit request/response types

type CreateHabitRequest struct {
	Title          string         `json:"title" binding:"required"`
	Description    string         `json:"description"`
	RecurrenceType RecurrenceType `json:"recurrence_type" binding:"required"`
	DaysOfWeek     int            `json:"days_of_week"`
	DayOfMonth     *int           `json:"day_of_month"`
	DueDate        *time.Time     `json:"due_date"`
	StartDate      *time.Time     `json:"start_date"`
	EndDate        *time.Time     `json:"end_date"`
}

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

type CompleteHabitRequest struct {
	Date  *time.Time `json:"date"`
	Notes string     `json:"notes"`
}

type HabitWithStatus struct {
	Habit
	IsCompleted bool       `json:"is_completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type HabitStatsResponse struct {
	CurrentStreak     int     `json:"current_streak"`
	CompletionRate7d  float64 `json:"completion_rate_7d"`
	CompletionRate30d float64 `json:"completion_rate_30d"`
	TotalCompletions  int     `json:"total_completions"`
}

// Habit errors

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

/******************************************************************************
 * MESSAGING
 ******************************************************************************/

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

/******************************************************************************
 * BACKGROUND JOBS
 ******************************************************************************/

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
	CreatedAt   time.Time  `gorm:"column:created_at;default:now()" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;default:now()" json:"updated_at"`
}

func (BackgroundJob) TableName() string {
	return "background_jobs"
}

type ProgressUpdate struct {
	Type     string `json:"type"` // 'progress', 'status', 'complete', 'error'
	JobID    string `json:"job_id"`
	Progress int    `json:"progress"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Data     any    `json:"data,omitempty"`
}

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

/******************************************************************************
 * NOTIFICATIONS
 ******************************************************************************/

type PushToken struct {
	TokenID    string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:token_id" json:"token_id"`
	UserID     string    `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	ExpoToken  string    `gorm:"column:expo_token;not null;unique" json:"expo_token"`
	Platform   string    `gorm:"column:platform" json:"platform"`
	DeviceID   string    `gorm:"column:device_id" json:"device_id,omitempty"`
	IsActive   bool      `gorm:"column:is_active;default:true" json:"is_active"`
	LastUsedAt time.Time `gorm:"column:last_used_at;default:now()" json:"last_used_at"`
	CreatedAt  time.Time `gorm:"column:created_at;default:now()" json:"created_at"`
}

func (PushToken) TableName() string {
	return "push_tokens"
}

type Notification struct {
	NotificationID string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:notification_id" json:"notification_id"`
	UserID         string         `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	Type           string         `gorm:"column:type;not null" json:"type"`
	Title          string         `gorm:"column:title;not null" json:"title"`
	Body           string         `gorm:"column:body;not null" json:"body"`
	Data           map[string]any `gorm:"type:jsonb;column:data;serializer:json" json:"data,omitempty"`
	Status         string         `gorm:"column:status;default:pending" json:"status"`
	SentAt         *time.Time     `gorm:"column:sent_at" json:"sent_at,omitempty"`
	ReadAt         *time.Time     `gorm:"column:read_at" json:"read_at,omitempty"`
	Error          string         `gorm:"column:error" json:"error,omitempty"`
	ClearedAt      *time.Time     `gorm:"column:cleared_at" json:"cleared_at,omitempty"`
	CreatedAt      time.Time      `gorm:"column:created_at;default:now()" json:"created_at"`
}

func (Notification) TableName() string {
	return "notifications"
}

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

/******************************************************************************
 * FEEDBACK
 ******************************************************************************/

type Feedback struct {
	FeedbackID string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:feedback_id" json:"feedback_id"`
	UserID     string    `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	Category   string    `gorm:"column:category;not null" json:"category"`
	Message    string    `gorm:"column:message;not null" json:"message"`
	AppVersion string    `gorm:"column:app_version" json:"app_version"`
	OS         string    `gorm:"column:os" json:"os"`
	Device     string    `gorm:"column:device" json:"device"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (Feedback) TableName() string {
	return "feedback"
}

type CreateFeedbackRequest struct {
	Category   string `json:"category" binding:"required,oneof=bug feature general"`
	Message    string `json:"message" binding:"required,min=10"`
	AppVersion string `json:"app_version"`
	OS         string `json:"os"`
	Device     string `json:"device"`
}

/******************************************************************************
 * NUDGES
 ******************************************************************************/

type Nudge struct {
	NudgeID    string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:nudge_id" json:"nudge_id"`
	SenderID   string    `gorm:"type:uuid;column:sender_id;not null" json:"sender_id"`
	ReceiverID string    `gorm:"type:uuid;column:receiver_id;not null" json:"receiver_id"`
	TargetType string    `gorm:"column:target_type;not null" json:"target_type"`
	TargetID   string    `gorm:"type:uuid;column:target_id;not null" json:"target_id"`
	CreatedAt  time.Time `gorm:"column:created_at;default:now()" json:"created_at"`

	Sender   *User `gorm:"foreignKey:SenderID;references:UserID" json:"sender,omitempty"`
	Receiver *User `gorm:"foreignKey:ReceiverID;references:UserID" json:"receiver,omitempty"`
}

func (Nudge) TableName() string {
	return "nudges"
}

type NudgeResponse struct {
	NudgeID     string    `json:"nudge_id"`
	Sender      *User     `json:"sender,omitempty"`
	Receiver    *User     `json:"receiver,omitempty"`
	TargetType  string    `json:"target_type"`
	TargetID    string    `json:"target_id"`
	TargetTitle string    `json:"target_title"`
	CreatedAt   time.Time `json:"created_at"`
}

const (
	NudgeTargetTypeRoadmap = "roadmap"
	NudgeTargetTypeHabit   = "habit"
	NudgeRateLimitPerDay   = 2
)

var (
	ErrNudgeRateLimitExceeded = errors.New("rate limit exceeded: max 2 nudges per day per target")
	ErrNotAnchor              = errors.New("you are not the anchor for this target")
)
