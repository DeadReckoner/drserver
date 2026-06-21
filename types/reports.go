package types

import (
	"errors"
	"time"
)

/******************************************************************************
 * REPORTS & BLOCKING (UGC safety - App Store Guideline 1.2)
 ******************************************************************************/

// ReportType discriminates the two kinds of report carried by the single
// reports table and the single POST /reports endpoint.
type ReportType string

const (
	ReportTypeUser ReportType = "user" // reporting another member / a message
	ReportTypeAI   ReportType = "ai"   // reporting AI-generated content
)

func (t ReportType) Validate() bool {
	switch t {
	case ReportTypeUser, ReportTypeAI:
		return true
	}
	return false
}

// ReportStatus tracks an internal review lifecycle. Reports land as "open" and
// are advanced by support/admin tooling.
type ReportStatus string

const (
	ReportStatusOpen      ReportStatus = "open"
	ReportStatusReviewing ReportStatus = "reviewing"
	ReportStatusActioned  ReportStatus = "actioned"
	ReportStatusDismissed ReportStatus = "dismissed"
)

// userReportReasons / aiReportReasons are the accepted reason codes per type.
// Kept as the single source of truth so the client and server agree.
var userReportReasons = map[string]bool{
	"harassment":            true,
	"spam":                  true,
	"inappropriate_content": true,
	"impersonation":         true,
	"other":                 true,
}

var aiReportReasons = map[string]bool{
	"unsafe":                true,
	"inaccurate":            true,
	"offensive":             true,
	"medical_mental_health": true,
	"legal_financial":       true,
	"other":                 true,
}

// ValidReason reports whether reason is accepted for the given report type.
func ValidReason(t ReportType, reason string) bool {
	switch t {
	case ReportTypeUser:
		return userReportReasons[reason]
	case ReportTypeAI:
		return aiReportReasons[reason]
	}
	return false
}

// Report mirrors a row in public.reports. A single, type-discriminated record:
// type='user' uses the reported_user_id/thread_id/message_id fields; type='ai'
// uses output_type/output_id/roadmap_id/job_id. The context snapshot captures
// the reported content so the report is actionable even if the source was
// never persisted (e.g. an unaccepted roadmap preview) or later deleted.
type Report struct {
	ReportID   string       `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:report_id" json:"report_id"`
	ReporterID string       `gorm:"type:uuid;column:reporter_id" json:"reporter_id"`
	Type       ReportType   `gorm:"column:type;not null" json:"type"`
	Reason     string       `gorm:"column:reason;not null" json:"reason"`
	Note       *string      `gorm:"column:note" json:"note,omitempty"`
	Status     ReportStatus `gorm:"column:status;not null;default:'open'" json:"status"`

	// type='user' references
	ReportedUserID *string `gorm:"type:uuid;column:reported_user_id" json:"reported_user_id,omitempty"`
	ThreadID       *string `gorm:"type:uuid;column:thread_id" json:"thread_id,omitempty"`
	MessageID      *string `gorm:"type:uuid;column:message_id" json:"message_id,omitempty"`

	// type='ai' references
	OutputType *string `gorm:"column:output_type" json:"output_type,omitempty"`
	OutputID   *string `gorm:"type:uuid;column:output_id" json:"output_id,omitempty"`
	RoadmapID  *string `gorm:"type:uuid;column:roadmap_id" json:"roadmap_id,omitempty"`
	JobID      *string `gorm:"type:uuid;column:job_id" json:"job_id,omitempty"`

	Context    map[string]any `gorm:"type:jsonb;column:context;serializer:json" json:"context,omitempty"`
	AppVersion string         `gorm:"column:app_version" json:"app_version,omitempty"`
	OS         string         `gorm:"column:os" json:"os,omitempty"`
	Device     string         `gorm:"column:device" json:"device,omitempty"`

	CreatedAt time.Time  `gorm:"column:created_at;default:now()" json:"created_at"`
	UpdatedAt *time.Time `gorm:"column:updated_at" json:"updated_at,omitempty"`
}

func (Report) TableName() string { return "reports" }

// maxReportNoteLen bounds the optional free-text note.
const maxReportNoteLen = 2000

// CreateReportRequest is the body for POST /api/v1/reports. Validation branches
// on Type, so it uses a Validate() receiver rather than binding tags alone.
type CreateReportRequest struct {
	Type   ReportType `json:"type"`
	Reason string     `json:"reason"`
	Note   *string    `json:"note"`

	// user-report fields
	ReportedUserID *string `json:"reported_user_id"`
	ThreadID       *string `json:"thread_id"`
	MessageID      *string `json:"message_id"`

	// ai-report fields
	OutputType *string `json:"output_type"`
	OutputID   *string `json:"output_id"`
	RoadmapID  *string `json:"roadmap_id"`
	JobID      *string `json:"job_id"`

	Context map[string]any `json:"context"`

	AppVersion string `json:"app_version"`
	OS         string `json:"os"`
	Device     string `json:"device"`
}

// Validate enforces the per-type contract. It does not check existence of
// referenced rows (the handler does that against the DB and the reporter id).
func (r CreateReportRequest) Validate() error {
	if !r.Type.Validate() {
		return errors.New("type must be 'user' or 'ai'")
	}
	if r.Reason == "" {
		return errors.New("reason is required")
	}
	if !ValidReason(r.Type, r.Reason) {
		return errors.New("reason is not valid for this report type")
	}
	if r.Note != nil && len(*r.Note) > maxReportNoteLen {
		return errors.New("note is too long")
	}

	switch r.Type {
	case ReportTypeUser:
		if r.ReportedUserID == nil || *r.ReportedUserID == "" {
			return errors.New("reported_user_id is required for a user report")
		}
	case ReportTypeAI:
		if r.OutputType == nil || *r.OutputType == "" {
			return errors.New("output_type is required for an AI report")
		}
		// AI previews are not always persisted, so output_id may be absent -
		// but then we need the content snapshot to make the report actionable.
		hasOutputID := r.OutputID != nil && *r.OutputID != ""
		if !hasOutputID && len(r.Context) == 0 {
			return errors.New("an AI report requires either output_id or context")
		}
	}
	return nil
}

// UserBlock mirrors a row in public.user_blocks. Directional: BlockerID blocked
// BlockedID. Enforcement treats a block in either direction as blocking, but
// only the blocker owns (and can remove) the row.
type UserBlock struct {
	BlockID   string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid();column:block_id" json:"block_id"`
	BlockerID string    `gorm:"type:uuid;column:blocker_id;not null" json:"blocker_id"`
	BlockedID string    `gorm:"type:uuid;column:blocked_id;not null" json:"blocked_id"`
	CreatedAt time.Time `gorm:"column:created_at;default:now()" json:"created_at"`
}

func (UserBlock) TableName() string { return "user_blocks" }

// BlockedUserResponse is one entry in GET /friends/blocked: the blocked user's
// profile plus when the block was created. Only the blocks the current user
// owns (blocker_id = me) are returned, since those are the ones they can lift.
type BlockedUserResponse struct {
	User      User      `json:"user"`
	BlockedAt time.Time `json:"blocked_at"`
}
