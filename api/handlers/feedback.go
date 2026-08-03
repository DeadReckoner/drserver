package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/email"
	"dr/server/log"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FeedbackHandler struct {
	db     *gorm.DB
	mailer *email.Dispatcher
}

func NewFeedbackHandler(db *gorm.DB, mailer *email.Dispatcher) *FeedbackHandler {
	return &FeedbackHandler{db: db, mailer: mailer}
}

// CreateFeedback handles POST /api/v1/feedback. It serves all four inbound
// contact categories - bug, feature, general, and support. The row is
// persisted first (the durable artifact), then a notification email is sent
// to the support inbox out-of-band so the team sees it without querying the
// database.
func (h *FeedbackHandler) CreateFeedback(c *gin.Context) {
	ctx := c.Request.Context()
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	var req types.CreateFeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}

	feedback := &types.Feedback{
		UserID:     userID.(string),
		Category:   req.Category,
		Subject:    strings.TrimSpace(req.Subject),
		Message:    req.Message,
		AppVersion: req.AppVersion,
		OS:         req.OS,
		Device:     req.Device,
	}

	if err := database.CreateFeedback(ctx, feedback); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": feedback})
	log.Info(ctx, "feedback created: "+feedback.FeedbackID)

	// Notify support out-of-band. Fresh context: the request context is
	// cancelled the moment we return the 201 above. A send failure is logged,
	// not fatal - the persisted row is the source of truth.
	go h.dispatchFeedbackEmail(*feedback)
}

// dispatchFeedbackEmail renders and sends the support notification for one
// piece of feedback.
func (h *FeedbackHandler) dispatchFeedbackEmail(feedback types.Feedback) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(feedback.UserID))

	vars := email.Vars{
		FeedbackID:        feedback.FeedbackID,
		FeedbackCategory:  feedback.Category,
		FeedbackSubject:   feedback.Subject,
		FeedbackMessage:   feedback.Message,
		FeedbackAppVer:    feedback.AppVersion,
		FeedbackOS:        feedback.OS,
		FeedbackDevice:    feedback.Device,
		FeedbackCreatedAt: feedback.CreatedAt.UTC().Format(time.RFC1123),
		SubmitterID:       feedback.UserID,
	}
	if u, err := database.GetUserByID(feedback.UserID); err == nil {
		vars.SubmitterName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		vars.SubmitterEmail = u.Email
	}

	// dedupeKey = feedback_id so a client retry never double-notifies support.
	// Reply-To is the submitter so the team can answer straight from the
	// notification.
	if err := h.mailer.SendToAddressWithReplyTo(
		ctx, supportInbox(), vars.SubmitterEmail, feedback.UserID,
		email.KindFeedbackReceived, vars, feedback.FeedbackID,
	); err != nil {
		log.Error(ctx, "failed to send feedback email", err)
	}
}
