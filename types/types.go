package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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

type StepStatus string

const (
	StepStatusIncomplete StepStatus = "incomplete"
	StepStatusCompleted  StepStatus = "completed"
	StepStatusSkipped    StepStatus = "skipped"
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

	ProfileImageURL string `gorm:"column:profile_image_url" json:"profile_image_url,omitempty"`

	// Timezone is the user's IANA timezone (e.g. "America/Los_Angeles").
	// Sent by the mobile client on auth via PATCH /users/:id. Used server-side
	// for quiet hours, 9am-local anchor batches, and digest delivery times.
	// Defaults to 'UTC' at the database level for safety.
	Timezone string `gorm:"column:timezone;not null;default:UTC" json:"timezone,omitempty"`
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
	AllowedHabits   []Habit   `json:"allowed_habits"`
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
	StartDate         *time.Time `json:"start_date,omitempty"`
	Type              string     `gorm:"type:roadmap_type;default:'custom_made'" json:"type"` // "ai_generated" or "custom_made"
	AnchorID          *string    `gorm:"type:uuid;column:anchor_id" json:"anchor_id,omitempty"`
	CurrentSkillLevel string     `gorm:"column:current_skill_level" json:"current_skill_level,omitempty"`

	// Async generation tracking
	JobID            *string `gorm:"type:uuid;column:job_id" json:"job_id,omitempty"`
	GenerationStatus string  `gorm:"type:generation_status;column:generation_status;default:'pending'" json:"generation_status"`

	// Lifecycle controls
	Archived   bool       `gorm:"default:false" json:"archived"`
	PausedAt   *time.Time `gorm:"column:paused_at" json:"paused_at,omitempty"`
	ShareToken *string    `gorm:"column:share_token;unique" json:"share_token,omitempty"`

	// Relationships
	Phases     []Phase     `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"phases,omitempty"`
	Milestones []Milestone `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"milestones,omitempty"`
}

// ComputeAllStatuses computes statuses for all tasks, milestones, and phases in this roadmap.
// This is the single source of truth for status computation.
//
// Logic:
//   - Tasks: Completed tasks are "completed". Any incomplete task whose date is before today
//     is "overdue" (overdue is independent of position, so a user behind on several tasks
//     sees all of them flagged). At most ONE task across the entire roadmap is "in_progress":
//     the first incomplete task in the first incomplete milestone, and only when it is not
//     itself overdue. All remaining incomplete tasks are "not_started".
//   - Milestones: Derived from child tasks. "completed" if all tasks done, "overdue" if any
//     task is overdue, "in_progress" if any task is in_progress, otherwise "not_started".
//   - Phases: Derived from child milestones using the same logic.
//
// ComputeAllStatuses judges "overdue" against the server's UTC calendar day.
// Prefer ComputeAllStatusesInLocation with the owning user's timezone so the
// overdue boundary matches the user's local day (and the notification jobs,
// which already compare against the user's timezone). Retained for callers
// that don't have a timezone in hand.
func (r *Roadmap) ComputeAllStatuses() {
	r.ComputeAllStatusesInLocation(time.UTC)
}

// ComputeAllStatusesInLocation is ComputeAllStatuses with an explicit calendar
// location for the overdue boundary. A task is overdue when its date is before
// "today" as observed in loc, so the flip happens at the user's local midnight
// rather than the server's. A nil loc is treated as UTC.
func (r *Roadmap) ComputeAllStatusesInLocation(loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	roadmapStarted := r.StartDate != nil
	foundCurrentTask := false
	foundCurrentMilestone := false

	// Helper to compute task statuses for a milestone.
	//
	// "Overdue" is an independent property: ANY incomplete task whose date is
	// before today is overdue, no matter where it sits in the sequence. The
	// "current task" is the first incomplete task; it shows in_progress only
	// when it is not itself overdue. This keeps the one-current-task model
	// (at most one in_progress across the roadmap) while still surfacing every
	// past-due task as overdue.
	computeMilestoneTaskStatuses := func(m *Milestone) {
		for i := range m.Tasks {
			task := &m.Tasks[i]
			switch {
			case task.Completed:
				task.Status = string(StatusCompleted)
			case !roadmapStarted:
				task.Status = string(StatusNotStarted)
			case isDateBeforeTodayInLocation(task.Date, loc):
				// Past due and incomplete → overdue, regardless of whether
				// this is the current task. The first incomplete task still
				// counts as "current" (consumes the in_progress slot) even
				// though it's displayed as overdue.
				task.Status = string(StatusOverdue)
				foundCurrentTask = true
			case !foundCurrentTask:
				// First incomplete, on-time task → the current task.
				foundCurrentTask = true
				task.Status = string(StatusInProgress)
			default:
				task.Status = string(StatusNotStarted)
			}
		}
	}

	// Helper to compute milestone status from its tasks
	// isCurrentMilestone indicates this is the first non-completed milestone in sequence
	computeMilestoneStatus := func(m *Milestone, isCurrentMilestone bool) string {
		if m.Reached {
			return string(StatusCompleted)
		}
		if len(m.Tasks) == 0 {
			// If this is the current milestone and roadmap has started,
			// mark as in_progress so the UI can show the generate tasks button
			if isCurrentMilestone && roadmapStarted {
				return string(StatusInProgress)
			}
			return string(StatusNotStarted)
		}

		allCompleted := true
		anyInProgress := false
		anyOverdue := false

		for _, t := range m.Tasks {
			if t.Status != string(StatusCompleted) {
				allCompleted = false
			}
			if t.Status == string(StatusInProgress) {
				anyInProgress = true
			}
			if t.Status == string(StatusOverdue) {
				anyOverdue = true
			}
		}

		if allCompleted {
			return string(StatusCompleted)
		}
		if anyOverdue {
			return string(StatusOverdue)
		}
		if anyInProgress {
			return string(StatusInProgress)
		}
		return string(StatusNotStarted)
	}

	// Helper to compute phase status from its milestones
	computePhaseStatus := func(p *Phase) string {
		if len(p.Milestones) == 0 {
			return string(StatusNotStarted)
		}

		allCompleted := true
		anyInProgress := false
		anyOverdue := false

		for _, m := range p.Milestones {
			if m.Status != string(StatusCompleted) {
				allCompleted = false
			}
			if m.Status == string(StatusInProgress) {
				anyInProgress = true
			}
			if m.Status == string(StatusOverdue) {
				anyOverdue = true
			}
		}

		if allCompleted {
			return string(StatusCompleted)
		}
		if anyOverdue {
			return string(StatusOverdue)
		}
		if anyInProgress {
			return string(StatusInProgress)
		}
		return string(StatusNotStarted)
	}

	// Step 1: Compute task statuses for milestones in phases (in order)
	for i := range r.Phases {
		for j := range r.Phases[i].Milestones {
			computeMilestoneTaskStatuses(&r.Phases[i].Milestones[j])
		}
	}

	// Step 2: Compute task statuses for top-level milestones (in order)
	// Skip milestones that belong to a phase (already processed in Step 1)
	for i := range r.Milestones {
		if r.Milestones[i].PhaseID != nil && *r.Milestones[i].PhaseID != "" {
			continue // Already processed with phases
		}
		computeMilestoneTaskStatuses(&r.Milestones[i])
	}

	// Step 3: Compute milestone statuses (after all tasks are computed)
	// Track the first non-completed milestone as the "current" one
	for i := range r.Phases {
		for j := range r.Phases[i].Milestones {
			m := &r.Phases[i].Milestones[j]
			isCurrentMilestone := !foundCurrentMilestone && !m.Reached
			m.Status = computeMilestoneStatus(m, isCurrentMilestone)
			// Mark as found if this milestone is not completed
			if m.Status != string(StatusCompleted) && !foundCurrentMilestone {
				foundCurrentMilestone = true
			}
		}
	}
	for i := range r.Milestones {
		if r.Milestones[i].PhaseID != nil && *r.Milestones[i].PhaseID != "" {
			continue // Already processed with phases
		}
		m := &r.Milestones[i]
		isCurrentMilestone := !foundCurrentMilestone && !m.Reached
		m.Status = computeMilestoneStatus(m, isCurrentMilestone)
		// Mark as found if this milestone is not completed
		if m.Status != string(StatusCompleted) && !foundCurrentMilestone {
			foundCurrentMilestone = true
		}
	}

	// Step 4: Compute phase statuses (after all milestones are computed)
	for i := range r.Phases {
		r.Phases[i].Status = computePhaseStatus(&r.Phases[i])
	}

	// Step 5: Sync top-level milestones with their phase counterparts
	// Build a map of milestone statuses and task statuses from phases
	milestoneStatusMap := make(map[string]string)
	taskStatusMap := make(map[string]string)
	for i := range r.Phases {
		for j := range r.Phases[i].Milestones {
			m := &r.Phases[i].Milestones[j]
			milestoneStatusMap[m.MilestoneID] = m.Status
			for k := range m.Tasks {
				taskStatusMap[m.Tasks[k].TaskID] = m.Tasks[k].Status
			}
		}
	}
	// Apply to top-level milestones
	for i := range r.Milestones {
		if status, ok := milestoneStatusMap[r.Milestones[i].MilestoneID]; ok {
			r.Milestones[i].Status = status
		}
		for j := range r.Milestones[i].Tasks {
			if status, ok := taskStatusMap[r.Milestones[i].Tasks[j].TaskID]; ok {
				r.Milestones[i].Tasks[j].Status = status
			}
		}
	}
}

type Phase struct {
	PhaseID       string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"phase_id"`
	RoadmapID     string `gorm:"type:uuid" json:"roadmap_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	OrderIndex    int    `json:"order_index"`
	DurationWeeks string `json:"duration_weeks"`
	Status        string `gorm:"default:not_started" json:"status,omitempty"`

	Milestones []Milestone `gorm:"foreignKey:PhaseID;constraint:OnDelete:SET NULL;" json:"milestones,omitempty"`
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
	Status      string     `json:"status,omitempty"`

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

// IsComplete returns true if all tasks in the milestone are completed
func (m *Milestone) IsComplete() bool {
	if m.Reached {
		return true
	}
	for _, t := range m.Tasks {
		if !t.Completed {
			return false
		}
	}
	return len(m.Tasks) > 0 // Empty milestone is not complete
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

// isDateBeforeTodayInLocation reports whether date's calendar day is before
// today's calendar day, with "today" observed in loc. The task date is read as
// its UTC calendar day (matching how dates are stored and how the notification
// SQL casts them with a UTC session), while "today" is taken in the user's
// location so the overdue boundary flips at the user's local midnight. Both
// sides are projected onto UTC midnight purely to compare calendar days.
// Returns false if date is nil; a nil loc is treated as UTC.
func isDateBeforeTodayInLocation(date *time.Time, loc *time.Location) bool {
	if date == nil {
		return false
	}
	if loc == nil {
		loc = time.UTC
	}
	now := Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	d := date.UTC()
	dateOnly := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return dateOnly.Before(today)
}

// LocationOrUTC parses an IANA timezone name (e.g. "America/Los_Angeles")
// into a *time.Location, returning UTC when tz is empty or does not parse.
// Mirrors the notification jobs' COALESCE(NULLIF(timezone,”),'UTC') fallback.
func LocationOrUTC(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

type Step struct {
	StepID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"step_id"`
	TaskID      string `gorm:"type:uuid" json:"task_id"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
	Status      string `gorm:"default:incomplete" json:"status"` // "incomplete", "completed", "skipped"
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

// MilestoneReviewAnswers captures the user's answers to the 7 hardcoded review
// questions shown in the mobile UI when completing a milestone. All fields are
// free-form strings; empty strings are allowed (user skipped that question).
type MilestoneReviewAnswers struct {
	CompletionStatus string `json:"completionStatus"` // Q1: did they complete everything / check off what was needed
	WhatWorked       string `json:"whatWorked"`       // Q2: what felt smooth/effective/motivating
	WhatDidntWork    string `json:"whatDidntWork"`    // Q3: what felt confusing/difficult/unnecessary
	Obstacles        string `json:"obstacles"`        // Q4: practical or personal obstacles
	PaceFeedback     string `json:"paceFeedback"`     // Q5: workload and pace feedback
	ClarityFeedback  string `json:"clarityFeedback"`  // Q6: were the instructions clear
	GoingForward     string `json:"goingForward"`     // Q7: preferences for next milestone + life changes
}

// GenerateNextMilestoneRequest is the request body for the async next-milestone
// endpoint. Embeds GoalRequest (same goal metadata as initial generation) plus
// the user's review answers for the prior milestone.
type GenerateNextMilestoneRequest struct {
	GoalRequest
	Review MilestoneReviewAnswers `json:"review"`
}

// MilestoneAdjustments is the AI's proposed adjustments to a milestone based
// on user review answers. Because OpenAI strict-schema mode requires every
// field be in `required`, we use sentinel values to mean "unchanged":
//   - empty string for strings
//   - 0 for integers
//   - empty array for arrays
//
// The top-level Adjusted flag is a second gate: callers should ignore all
// fields unless Adjusted is true.
type MilestoneAdjustments struct {
	Adjusted          bool    `json:"adjusted"`
	SubGoal           string  `json:"sub_goal,omitempty"`
	Description       string  `json:"description,omitempty"`
	AcceptanceTest    string  `json:"acceptance_test,omitempty"`
	DurationDaysMin   int     `json:"duration_days_min,omitempty"`
	DurationDaysMax   int     `json:"duration_days_max,omitempty"`
	ResourcesForecast string  `json:"resources_forecast,omitempty"`
	Spikes            []Spike `json:"spikes,omitempty"` // non-empty = replace existing
	Risks             []Risk  `json:"risks,omitempty"`  // non-empty = replace existing
	Rationale         string  `json:"rationale,omitempty"`
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

/******************************************************************************
 * ROADMAP LIFECYCLE
 ******************************************************************************/

type ShiftDatesRequest struct {
	Days int `json:"days" binding:"required"`
}

type RealignDatesRequest struct {
	OffsetDays int `json:"offset_days"` // Default 0 = realign to today
}

type RealignDatesResponse struct {
	Roadmap     Roadmap `json:"roadmap"`
	DaysShifted int     `json:"days_shifted"`
}

type RestoreRoadmapRequest struct {
	StartFromToday bool `json:"start_from_today"`
}

/******************************************************************************
 * NOTIFICATION SETTINGS
 ******************************************************************************/

// DigestFrequency controls when batched notifications are delivered.
type DigestFrequency string

const (
	DigestFrequencyNone   DigestFrequency = "none"
	DigestFrequencyDaily  DigestFrequency = "daily"
	DigestFrequencyWeekly DigestFrequency = "weekly"
)

// DeliveryMode is how a single notification type is delivered.
type DeliveryMode string

const (
	DeliveryImmediate DeliveryMode = "immediate"
	DeliveryDigest    DeliveryMode = "digest"
)

// NotificationChannel is the transport for a notification.
const (
	NotificationChannelPush  = "push"
	NotificationChannelEmail = "email"
)

// NotificationTypePref is one notification type's enabled flag + delivery mode.
type NotificationTypePref struct {
	Enabled  bool         `json:"enabled"`
	Delivery DeliveryMode `json:"delivery"`
}

// MyGoalsPrefs holds per-type prefs for the user's own goals.
// JSON keys MUST match the SQL DEFAULT for notification_settings.my_goals_prefs.
type MyGoalsPrefs struct {
	DailyTaskReminder      NotificationTypePref `json:"daily_task_reminder"`
	TaskDueReminder        NotificationTypePref `json:"task_due_reminder"`
	MissedTask             NotificationTypePref `json:"missed_task"`
	FallingBehind          NotificationTypePref `json:"falling_behind"`
	MilestoneDue           NotificationTypePref `json:"milestone_due"`
	MilestoneAtRisk        NotificationTypePref `json:"milestone_at_risk"`
	FollowOnMilestoneReady NotificationTypePref `json:"follow_on_milestone_ready"`
	GoalMilestoneCompleted NotificationTypePref `json:"goal_milestone_completed"`

	// AgentUpdates covers all AI-generation outcome pushes (roadmap and
	// next-milestone, success and failure). Users think of these as one
	// thing — "tell me when my generation finishes" — so the four distinct
	// notification-type strings all resolve to this single toggle in Lookup.
	AgentUpdates NotificationTypePref `json:"agent_updates"`
}

// Scan implements sql.Scanner so GORM can read JSONB into a typed struct.
func (p *MyGoalsPrefs) Scan(value any) error {
	if value == nil {
		*p = MyGoalsPrefs{}
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("MyGoalsPrefs.Scan: unsupported type %T", value)
	}
	if err := json.Unmarshal(b, p); err != nil {
		return err
	}
	p.fillMissing()
	return nil
}

// fillMissing backfills any pref key absent from the stored JSONB with its
// system default. A real stored pref always has Delivery set ("immediate" or
// "digest"), so an empty Delivery reliably marks a key that didn't exist when
// the row was written — which is exactly what happens to existing users when a
// new notification type is added. This makes adding a type a code-only change;
// the SQL backfill migration is belt-and-suspenders for fresh SQL inserts.
func (p *MyGoalsPrefs) fillMissing() {
	d := DefaultMyGoalsPrefs()
	if p.DailyTaskReminder.Delivery == "" {
		p.DailyTaskReminder = d.DailyTaskReminder
	}
	if p.TaskDueReminder.Delivery == "" {
		p.TaskDueReminder = d.TaskDueReminder
	}
	if p.MissedTask.Delivery == "" {
		p.MissedTask = d.MissedTask
	}
	if p.FallingBehind.Delivery == "" {
		p.FallingBehind = d.FallingBehind
	}
	if p.MilestoneDue.Delivery == "" {
		p.MilestoneDue = d.MilestoneDue
	}
	if p.MilestoneAtRisk.Delivery == "" {
		p.MilestoneAtRisk = d.MilestoneAtRisk
	}
	if p.FollowOnMilestoneReady.Delivery == "" {
		p.FollowOnMilestoneReady = d.FollowOnMilestoneReady
	}
	if p.GoalMilestoneCompleted.Delivery == "" {
		p.GoalMilestoneCompleted = d.GoalMilestoneCompleted
	}
	if p.AgentUpdates.Delivery == "" {
		p.AgentUpdates = d.AgentUpdates
	}
}

// Value implements driver.Valuer so GORM can write the typed struct to JSONB.
func (p MyGoalsPrefs) Value() (driver.Value, error) { return json.Marshal(p) }

// Lookup returns the pref for a notification type name (the JSON tag of one
// of the struct fields, e.g. "missed_task"). Returns false if the name does
// not correspond to a known my-goals notification type.
func (p MyGoalsPrefs) Lookup(notificationType string) (NotificationTypePref, bool) {
	switch notificationType {
	case "daily_task_reminder":
		return p.DailyTaskReminder, true
	case "task_due_reminder":
		return p.TaskDueReminder, true
	case "missed_task":
		return p.MissedTask, true
	case "falling_behind":
		return p.FallingBehind, true
	case "milestone_due":
		return p.MilestoneDue, true
	case "milestone_at_risk":
		return p.MilestoneAtRisk, true
	case "follow_on_milestone_ready":
		return p.FollowOnMilestoneReady, true
	case "goal_milestone_completed":
		return p.GoalMilestoneCompleted, true
	// All four agent-generation outcomes share the single agent_updates toggle.
	case "agent_updates", "roadmap_complete", "roadmap_failed",
		"next_milestone_complete", "next_milestone_failed":
		return p.AgentUpdates, true
	}
	return NotificationTypePref{}, false
}

// FriendsAndAccountabilityPrefs holds per-type prefs for the F&A context.
// JSON keys MUST match the SQL DEFAULT for notification_settings.f_and_a_prefs.
type FriendsAndAccountabilityPrefs struct {
	FriendRequest         NotificationTypePref `json:"friend_request"`
	AnchorRequest         NotificationTypePref `json:"anchor_request"`
	AnchorAcceptedRemoved NotificationTypePref `json:"anchor_accepted_removed"`
	MissedProgress        NotificationTypePref `json:"missed_progress"`
	FallingBehind         NotificationTypePref `json:"falling_behind"`
	MilestoneAtRisk       NotificationTypePref `json:"milestone_at_risk"`
	MilestoneCompleted    NotificationTypePref `json:"milestone_completed"`
	GoalCompleted         NotificationTypePref `json:"goal_completed"`
	TimelineUpdated       NotificationTypePref `json:"timeline_updated"`

	// Nudges covers both roadmap and habit nudges from an accountability partner.
	Nudges NotificationTypePref `json:"nudges"`
	// Messages covers new-message pushes. Conversational, so Dispatch treats
	// this type specially: exempt from the frequency cap and dropped (not
	// deferred) during quiet hours. See notifications.capExempt/quietHoursDrop.
	Messages NotificationTypePref `json:"messages"`
}

func (p *FriendsAndAccountabilityPrefs) Scan(value any) error {
	if value == nil {
		*p = FriendsAndAccountabilityPrefs{}
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("FriendsAndAccountabilityPrefs.Scan: unsupported type %T", value)
	}
	if err := json.Unmarshal(b, p); err != nil {
		return err
	}
	p.fillMissing()
	return nil
}

// fillMissing backfills absent pref keys with their system defaults. See
// MyGoalsPrefs.fillMissing for the rationale (empty Delivery == missing key).
func (p *FriendsAndAccountabilityPrefs) fillMissing() {
	d := DefaultFAndAPrefs()
	if p.FriendRequest.Delivery == "" {
		p.FriendRequest = d.FriendRequest
	}
	if p.AnchorRequest.Delivery == "" {
		p.AnchorRequest = d.AnchorRequest
	}
	if p.AnchorAcceptedRemoved.Delivery == "" {
		p.AnchorAcceptedRemoved = d.AnchorAcceptedRemoved
	}
	if p.MissedProgress.Delivery == "" {
		p.MissedProgress = d.MissedProgress
	}
	if p.FallingBehind.Delivery == "" {
		p.FallingBehind = d.FallingBehind
	}
	if p.MilestoneAtRisk.Delivery == "" {
		p.MilestoneAtRisk = d.MilestoneAtRisk
	}
	if p.MilestoneCompleted.Delivery == "" {
		p.MilestoneCompleted = d.MilestoneCompleted
	}
	if p.GoalCompleted.Delivery == "" {
		p.GoalCompleted = d.GoalCompleted
	}
	if p.TimelineUpdated.Delivery == "" {
		p.TimelineUpdated = d.TimelineUpdated
	}
	if p.Nudges.Delivery == "" {
		p.Nudges = d.Nudges
	}
	if p.Messages.Delivery == "" {
		p.Messages = d.Messages
	}
}

func (p FriendsAndAccountabilityPrefs) Value() (driver.Value, error) {
	return json.Marshal(p)
}

// Lookup returns the pref for a notification type name (the JSON tag of one
// of the struct fields, e.g. "missed_progress"). Returns false if the name
// does not correspond to a known F&A notification type.
func (p FriendsAndAccountabilityPrefs) Lookup(notificationType string) (NotificationTypePref, bool) {
	switch notificationType {
	case "friend_request":
		return p.FriendRequest, true
	case "anchor_request":
		return p.AnchorRequest, true
	case "anchor_accepted_removed":
		return p.AnchorAcceptedRemoved, true
	case "missed_progress":
		return p.MissedProgress, true
	case "falling_behind":
		return p.FallingBehind, true
	case "milestone_at_risk":
		return p.MilestoneAtRisk, true
	case "milestone_completed":
		return p.MilestoneCompleted, true
	case "goal_completed":
		return p.GoalCompleted, true
	case "timeline_updated":
		return p.TimelineUpdated, true
	case "nudges", "roadmap_nudge", "habit_nudge":
		return p.Nudges, true
	case "messages", "message_new":
		return p.Messages, true
	// Shared toggles: request follow-ups ride the original request's pref.
	case "friend_accepted", "friend_rejected":
		return p.FriendRequest, true
	case "anchor_rejected":
		return p.AnchorAcceptedRemoved, true
	case "habit_anchor_request":
		return p.AnchorRequest, true
	}
	return NotificationTypePref{}, false
}

// DefaultMyGoalsPrefs returns the system default prefs for a user's own goals.
// MUST match the SQL DEFAULT on notification_settings.my_goals_prefs.
func DefaultMyGoalsPrefs() MyGoalsPrefs {
	immediate := NotificationTypePref{Enabled: true, Delivery: DeliveryImmediate}
	digest := NotificationTypePref{Enabled: true, Delivery: DeliveryDigest}
	return MyGoalsPrefs{
		DailyTaskReminder:      immediate,
		TaskDueReminder:        immediate,
		MissedTask:             digest, // routine; default delivery is next-morning batch
		FallingBehind:          digest,
		MilestoneDue:           immediate,
		MilestoneAtRisk:        immediate, // major status change
		FollowOnMilestoneReady: immediate,
		GoalMilestoneCompleted: immediate,
		AgentUpdates:           immediate, // generation finished — wants to know now
	}
}

// DefaultFAndAPrefs returns the system default prefs for Friends & Accountability.
// MUST match the SQL DEFAULT on notification_settings.f_and_a_prefs.
func DefaultFAndAPrefs() FriendsAndAccountabilityPrefs {
	immediate := NotificationTypePref{Enabled: true, Delivery: DeliveryImmediate}
	digest := NotificationTypePref{Enabled: true, Delivery: DeliveryDigest}
	return FriendsAndAccountabilityPrefs{
		FriendRequest:         immediate, // major status change
		AnchorRequest:         immediate,
		AnchorAcceptedRemoved: immediate,
		MissedProgress:        digest, // routine; batched/delayed by default
		FallingBehind:         digest,
		MilestoneAtRisk:       immediate,
		MilestoneCompleted:    digest,
		GoalCompleted:         immediate,
		TimelineUpdated:       digest,
		Nudges:                immediate, // a partner reached out — time-sensitive
		Messages:              immediate, // conversational; cap-exempt in Dispatch
	}
}

// NotificationSettings is the user's top-level notification preferences row.
// Exactly one row per user — created via trigger on user insert.
type NotificationSettings struct {
	UserID string `gorm:"type:uuid;primaryKey;column:user_id" json:"user_id"`

	// Channels
	PushEnabled  bool `gorm:"column:push_enabled;not null;default:true" json:"push_enabled"`
	EmailEnabled bool `gorm:"column:email_enabled;not null;default:true" json:"email_enabled"`

	// Quiet hours (interpreted in the user's timezone — see users.timezone)
	QuietHoursEnabled bool   `gorm:"column:quiet_hours_enabled;not null;default:false" json:"quiet_hours_enabled"`
	QuietHoursStart   string `gorm:"column:quiet_hours_start;not null;default:'22:00'" json:"quiet_hours_start"`
	QuietHoursEnd     string `gorm:"column:quiet_hours_end;not null;default:'08:00'" json:"quiet_hours_end"`

	// Digest
	DigestFrequency         DigestFrequency `gorm:"column:digest_frequency;not null;default:'none'" json:"digest_frequency"`
	DigestDeliveryHour      int             `gorm:"column:digest_delivery_hour;not null;default:8" json:"digest_delivery_hour"`
	DigestDeliveryDayOfWeek int             `gorm:"column:digest_delivery_day_of_week;not null;default:1" json:"digest_delivery_day_of_week"`

	// Global mute — when set to a future time, every notification skips
	GlobalMuteUntil *time.Time `gorm:"column:global_mute_until" json:"global_mute_until,omitempty"`

	// Per-type prefs — DB has full default JSON blob; always fully populated.
	MyGoalsPrefs MyGoalsPrefs `gorm:"type:jsonb;column:my_goals_prefs;not null" json:"my_goals_prefs"`

	// Friends & Accountability
	FAndAEnabled      bool                          `gorm:"column:f_and_a_enabled;not null;default:true" json:"f_and_a_enabled"`
	ApplyToAllFriends bool                          `gorm:"column:apply_to_all_friends;not null;default:true" json:"apply_to_all_friends"`
	FAndAPrefs        FriendsAndAccountabilityPrefs `gorm:"type:jsonb;column:f_and_a_prefs;not null" json:"f_and_a_prefs"`

	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (NotificationSettings) TableName() string { return "notification_settings" }

// PrefOverrides is the partial-JSON shape stored in notification_overrides.prefs.
// Keyed by notification type name (e.g. "missed_progress"); only fields present
// in the map override the user's F&A defaults — others fall through.
type PrefOverrides map[string]NotificationTypePref

func (p *PrefOverrides) Scan(value any) error {
	if value == nil {
		*p = PrefOverrides{}
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("PrefOverrides.Scan: unsupported type %T", value)
	}
	return json.Unmarshal(b, p)
}

func (p PrefOverrides) Value() (driver.Value, error) {
	if p == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(p)
}

// NotificationOverride is a per-person OR per-anchored-goal override on the
// user's F&A prefs. Exactly one of FriendID / AnchoredRoadmapID is non-nil
// (enforced by DB check constraint).
type NotificationOverride struct {
	ID                string        `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:id" json:"id"`
	UserID            string        `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	FriendID          *string       `gorm:"type:uuid;column:friend_id" json:"friend_id,omitempty"`
	AnchoredRoadmapID *string       `gorm:"type:uuid;column:anchored_roadmap_id" json:"anchored_roadmap_id,omitempty"`
	Prefs             PrefOverrides `gorm:"type:jsonb;column:prefs;not null" json:"prefs"`
	MuteUntil         *time.Time    `gorm:"column:mute_until" json:"mute_until,omitempty"`
	CreatedAt         time.Time     `gorm:"column:created_at" json:"created_at"`
	UpdatedAt         time.Time     `gorm:"column:updated_at" json:"updated_at"`
}

func (NotificationOverride) TableName() string { return "notification_overrides" }

// NotificationDeliveryLog records every notification delivery. The unique
// constraint on (user_id, notification_type, dedup_key, channel) is the
// entire idempotency guarantee: senders do INSERT ... ON CONFLICT DO NOTHING
// and only proceed if a row was inserted.
type NotificationDeliveryLog struct {
	ID               string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:id" json:"id"`
	UserID           string    `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	NotificationType string    `gorm:"column:notification_type;not null" json:"notification_type"`
	DedupKey         string    `gorm:"column:dedup_key;not null" json:"dedup_key"`
	Channel          string    `gorm:"column:channel;not null" json:"channel"` // 'push' | 'email'
	DeliveredAt      time.Time `gorm:"column:delivered_at;not null;default:now()" json:"delivered_at"`
}

func (NotificationDeliveryLog) TableName() string { return "notification_delivery_log" }

// PendingNotification is a notification the resolver said to DEFER (typically
// because the recipient is in quiet hours when an immediate-delivery push
// would fire). The DeliverPendingNotifications cron picks up rows where
// ScheduledFor <= now() and re-runs Dispatch on them, deleting on success.
//
// The unique constraint on (user_id, notification_type, dedup_key, channel)
// mirrors notification_delivery_log so duplicate triggers during a deferral
// don't pile up — they UPSERT the existing row's ScheduledFor.
type PendingNotification struct {
	ID               string `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:id" json:"id"`
	UserID           string `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	NotificationType string `gorm:"column:notification_type;not null" json:"notification_type"`
	DedupKey         string `gorm:"column:dedup_key;not null" json:"dedup_key"`
	Channel          string `gorm:"column:channel;not null" json:"channel"`

	// Routing context for re-evaluation by the resolver at delivery time.
	Category          string  `gorm:"column:category;not null" json:"category"`
	FriendID          *string `gorm:"type:uuid;column:friend_id" json:"friend_id,omitempty"`
	AnchoredRoadmapID *string `gorm:"type:uuid;column:anchored_roadmap_id" json:"anchored_roadmap_id,omitempty"`

	// Payload to send. Title/body are captured at queue time; Data is JSONB
	// so it can hold whatever shape the caller put in DispatchOptions.Data.
	Title string         `gorm:"column:title;not null" json:"title"`
	Body  string         `gorm:"column:body;not null" json:"body"`
	Data  map[string]any `gorm:"type:jsonb;column:data;not null" json:"data"`

	ScheduledFor time.Time `gorm:"column:scheduled_for;not null" json:"scheduled_for"`
	Attempts     int       `gorm:"column:attempts;not null;default:0" json:"attempts"`
	CreatedAt    time.Time `gorm:"column:created_at;not null;default:now()" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;not null;default:now()" json:"updated_at"`
}

func (PendingNotification) TableName() string { return "pending_notifications" }

// DigestItem is a notification whose resolver decision was DeliveryDigest.
// Dispatch writes here instead of pushing immediately; the
// DeliverDueDigests cron batches the user's queued items into a single
// daily or weekly email when their digest hour comes around.
//
// The unique constraint on (user_id, notification_type, dedup_key) mirrors
// notification_delivery_log so duplicate scan ticks don't produce duplicate
// items in the same digest.
type DigestItem struct {
	ID               string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:id" json:"id"`
	UserID           string         `gorm:"type:uuid;column:user_id;not null" json:"user_id"`
	NotificationType string         `gorm:"column:notification_type;not null" json:"notification_type"`
	DedupKey         string         `gorm:"column:dedup_key;not null" json:"dedup_key"`
	Title            string         `gorm:"column:title;not null" json:"title"`
	Body             string         `gorm:"column:body;not null" json:"body"`
	Data             map[string]any `gorm:"type:jsonb;column:data;not null" json:"data"`
	CreatedAt        time.Time      `gorm:"column:created_at;not null;default:now()" json:"created_at"`
}

func (DigestItem) TableName() string { return "digest_items" }
