package email

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dr/server/database"
	"dr/server/log"
	"dr/server/types"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Dispatcher orchestrates "find user → render template → send → log". Pulls
// the resend client and DB out of struct fields so tests can swap them.
type Dispatcher struct {
	DB     *gorm.DB
	Client *ResendClient
}

// NewDispatcher wires production defaults: the package-global db is used,
// and a Resend client is constructed from env vars.
func NewDispatcher(db *gorm.DB) *Dispatcher {
	return &Dispatcher{
		DB:     db,
		Client: NewResendClient(),
	}
}

// Send is the main entry point. It:
//  1. Resolves the user's email + first name (skip if no user record)
//  2. Dedupes on (user_id, kind, dedupeKey) when dedupeKey != ""
//  3. Renders the template
//  4. Sends through Resend with retry
//  5. Records the outcome in email_log
//
// All errors are returned but never panic. Callers that fire-and-forget (the
// webhook handler) should log them and move on - the email_log row is the
// durable artifact, not the function return.
func (d *Dispatcher) Send(ctx context.Context, userID string, kind EmailKind, vars Vars, dedupeKey string) error {
	// 1. Look up user.
	var user types.User
	if err := d.DB.Where("user_id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("email: user %s not found", userID)
		}
		return fmt.Errorf("email: load user: %w", err)
	}
	if user.Email == "" {
		// Can't send without an address. Record the skip for audit.
		d.recordSkip(userID, "", kind, dedupeKey, "no email on user record")
		return errors.New("email: user has no email address")
	}

	// Fill in vars defaults from the user record (callers can override).
	if vars.FirstName == "" {
		vars.FirstName = user.FirstName
	}
	if vars.Email == "" {
		vars.Email = user.Email
	}

	// 2. Dedupe.
	if dedupeKey != "" {
		alreadySent, err := database.EmailAlreadySent(d.DB, userID, string(kind), dedupeKey)
		if err != nil {
			log.Warn(ctx, "email: dedupe check failed", err.Error())
			// Fall through - on dedupe failure we'd rather send a duplicate than
			// silently drop a transactional email.
		}
		if alreadySent {
			d.recordSkip(userID, user.Email, kind, dedupeKey, "already sent with same dedupe_key")
			return nil
		}
	}

	// 3. Render.
	subject, htmlBody, err := Render(kind, vars)
	if err != nil {
		d.recordError(userID, user.Email, kind, dedupeKey, "render: "+err.Error())
		return fmt.Errorf("email: render: %w", err)
	}

	// 4. Send.
	id, sendErr := d.Client.Send(ctx, SendRequest{
		From:    d.Client.From,
		To:      []string{user.Email},
		Subject: subject,
		HTML:    htmlBody,
	})

	// 5. Log.
	row := &types.EmailLog{
		ID:        uuid.NewString(),
		UserID:    userID,
		Kind:      string(kind),
		ToEmail:   user.Email,
		DedupeKey: dedupeKey,
		CreatedAt: time.Now().UTC(),
	}
	if sendErr != nil {
		row.Status = string(StatusError)
		row.ErrorText = sendErr.Error()
	} else {
		row.Status = string(StatusSent)
		row.ProviderMessageID = id
	}
	if logErr := database.InsertEmailLog(d.DB, row); logErr != nil {
		log.Error(ctx, "email: failed to write email_log", logErr)
	}

	return sendErr
}

// recordSkip writes a status=skipped row for visibility.
func (d *Dispatcher) recordSkip(userID, toEmail string, kind EmailKind, dedupeKey, reason string) {
	row := &types.EmailLog{
		ID:        uuid.NewString(),
		UserID:    userID,
		Kind:      string(kind),
		ToEmail:   toEmail,
		Status:    string(StatusSkipped),
		ErrorText: reason,
		DedupeKey: dedupeKey,
		CreatedAt: time.Now().UTC(),
	}
	_ = database.InsertEmailLog(d.DB, row)
}

// recordError writes a status=error row when something fails before send
// (e.g. template render error).
func (d *Dispatcher) recordError(userID, toEmail string, kind EmailKind, dedupeKey, reason string) {
	row := &types.EmailLog{
		ID:        uuid.NewString(),
		UserID:    userID,
		Kind:      string(kind),
		ToEmail:   toEmail,
		Status:    string(StatusError),
		ErrorText: reason,
		DedupeKey: dedupeKey,
		CreatedAt: time.Now().UTC(),
	}
	_ = database.InsertEmailLog(d.DB, row)
}
