package types

import "time"

/******************************************************************************
 * HABIT STEPS (multi-step recurring habits/tasks)
 ******************************************************************************/

// A recurring item (types.Habit) is a TEMPLATE. Each date it is due is an
// OCCURRENCE, identified by (HabitID, Date) -- occurrences are never stored as
// rows of their own; HabitCompletion exists only once an occurrence is done.
//
// That split drives the two types here, and mirrors the roadmap model as
// closely as the recurrence allows:
//
//	roadmap: Task  --has-->  Step        (state lives on the step itself)
//	habit:   Habit --has-->  HabitStep   (definition only, on the template)
//	                     +   HabitStepCompletion  (state, per occurrence)
//
// A roadmap task happens once, so its Step can carry Completed directly. A
// habit repeats, so step state has to hang off the occurrence instead -- if it
// lived on HabitStep, yesterday's checkmarks would leak into today.

// HabitStep is one step DEFINITION on a recurring habit/task template.
// Analogous to Step for roadmap tasks, minus the completion state.
type HabitStep struct {
	HabitStepID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"habit_step_id"`
	HabitID     string `gorm:"type:uuid" json:"habit_id"`
	UserID      string `gorm:"type:uuid" json:"user_id"`
	Description string `json:"description"`
	// Number is the explicit display order. Stable identity lives on
	// HabitStepID, never on Number, so reordering can never transfer
	// completion state between steps.
	Number int `json:"number"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (HabitStep) TableName() string { return "habit_steps" }

// HabitStepCompletion is the state of one step on one OCCURRENCE, plus a
// snapshot of how that step read at the time the occurrence materialised.
//
// The snapshot (Description, Number) is what keeps history honest: renaming or
// reordering a template step later must not rewrite what a completed occurrence
// says happened. HabitStepID goes NULL if the template step is deleted, for the
// same reason.
type HabitStepCompletion struct {
	HabitStepCompletionID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"habit_step_completion_id"`
	HabitID               string `gorm:"type:uuid" json:"habit_id"`
	UserID                string `gorm:"type:uuid" json:"user_id"`

	// Date identifies the occurrence. Always normalised to UTC midnight, the
	// same convention HabitCompletion.Date uses.
	Date time.Time `gorm:"type:date" json:"date"`

	// HabitStepID is nil for rows whose template step has since been deleted.
	HabitStepID *string `gorm:"type:uuid" json:"habit_step_id,omitempty"`

	Description string `json:"description"`
	Number      int    `json:"number"`

	// Status mirrors Step.Status exactly: "incomplete", "completed" or
	// "skipped". Completed is kept in sync with it, as Step does, so existing
	// clients that only understand a boolean keep working.
	Status      string     `gorm:"default:incomplete" json:"status"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (HabitStepCompletion) TableName() string { return "habit_step_completions" }

// IsDone reports whether the step counts towards completing its occurrence.
// "skipped" counts as done, matching shouldCompleteTask for roadmap steps.
func (s *HabitStepCompletion) IsDone() bool {
	return s.Status == string(StepStatusCompleted) || s.Status == string(StepStatusSkipped)
}

// HabitOccurrenceStep is the per-occurrence step as served to clients. It is a
// projection, not a table: for an occurrence that has never been touched there
// are no HabitStepCompletion rows at all and the steps are projected straight
// off the template as all-incomplete.
type HabitOccurrenceStep struct {
	// HabitStepID is the stable identifier clients send back when toggling.
	// Empty only for historical rows whose template step was deleted.
	HabitStepID string     `json:"habit_step_id"`
	Description string     `json:"description"`
	Number      int        `json:"number"`
	Status      string     `json:"status"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// HabitOccurrenceSteps is the full step state of one occurrence, including the
// progress counters the UI shows ("2 of 3"). StepsTotal == 0 means the item has
// no steps and keeps the plain single-action completion behaviour.
type HabitOccurrenceSteps struct {
	HabitID        string                `json:"habit_id"`
	Date           string                `json:"date"`
	Steps          []HabitOccurrenceStep `json:"steps"`
	StepsTotal     int                   `json:"steps_total"`
	StepsCompleted int                   `json:"steps_completed"`
	// IsCompleted reflects the parent occurrence, i.e. whether a
	// HabitCompletion row exists for (habit_id, date).
	IsCompleted bool `json:"is_completed"`
}

/******************************************************************************
 * HABIT STEP REQUESTS
 ******************************************************************************/

// CreateHabitStepRequest adds one step to a template.
type CreateHabitStepRequest struct {
	Description string `json:"description" binding:"required"`
	Number      int    `json:"number"`
}

// UpdateHabitStepRequest edits one step DEFINITION on the template. It never
// carries completion state -- that is per-occurrence and goes through
// SetHabitStepStateRequest.
type UpdateHabitStepRequest struct {
	Description *string `json:"description"`
	Number      *int    `json:"number"`
}

// SetHabitStepStateRequest toggles one step on one occurrence.
//
// Either Status ("incomplete"/"completed"/"skipped") or the legacy Completed
// boolean may be supplied; Status wins when both are present. This is the same
// dual accepted by the roadmap step endpoint.
type SetHabitStepStateRequest struct {
	// Date selects the occurrence; defaults to today when omitted.
	Date      *time.Time `json:"date"`
	Status    *string    `json:"status"`
	Completed *bool      `json:"completed"`
}

// Resolve collapses Status/Completed into a single status value.
func (r *SetHabitStepStateRequest) Resolve() (string, error) {
	if r.Status != nil {
		switch *r.Status {
		case string(StepStatusIncomplete), string(StepStatusCompleted), string(StepStatusSkipped):
			return *r.Status, nil
		default:
			return "", ErrInvalidStepStatus
		}
	}
	if r.Completed != nil {
		if *r.Completed {
			return string(StepStatusCompleted), nil
		}
		return string(StepStatusIncomplete), nil
	}
	return "", ErrStepStateRequired
}

// ReorderHabitStepsRequest assigns explicit display order in one call, so a
// drag-to-reorder gesture is a single atomic request rather than N PATCHes.
type ReorderHabitStepsRequest struct {
	StepIDs []string `json:"step_ids" binding:"required"`
}

var (
	ErrHabitStepNotFound = HabitError{"habit step not found"}
	ErrInvalidStepStatus = HabitError{"invalid step status"}
	ErrStepStateRequired = HabitError{"status or completed is required"}
)
