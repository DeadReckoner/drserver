package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
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

// maxReportsPerHour caps how many reports a single reporter can file in a
// rolling hour, to blunt spam/abuse of the endpoint.
const maxReportsPerHour = 10

// defaultSupportEmail is where inbound user contact is routed when
// SUPPORT_EMAIL is unset.
const defaultSupportEmail = "info@deadreckoner.io"

// supportInbox is the single destination for every inbound user-contact
// channel: reports, feedback, and support requests. Colocating them is
// deliberate - the team triages all of it from one mailbox.
func supportInbox() string {
	if to := os.Getenv("SUPPORT_EMAIL"); to != "" {
		return to
	}
	return defaultSupportEmail
}

type ReportHandler struct {
	db     *gorm.DB
	mailer *email.Dispatcher
}

func NewReportHandler(db *gorm.DB, mailer *email.Dispatcher) *ReportHandler {
	return &ReportHandler{db: db, mailer: mailer}
}

// CreateReport handles POST /api/v1/reports. One endpoint serves both report
// types; validation branches on type. The report is persisted first (the
// durable artifact), then a notification email is sent to support out-of-band.
func (h *ReportHandler) CreateReport(c *gin.Context) {
	ctx := c.Request.Context()
	reporterID := c.GetString("user_id")
	if reporterID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	var req types.CreateReportRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}

	// Type-specific cross-checks against the database.
	if req.Type == types.ReportTypeUser {
		if *req.ReportedUserID == reporterID {
			utils.BadRequestResponse(c, "you cannot report yourself")
			return
		}
		if _, err := database.GetUserByID(*req.ReportedUserID); err != nil {
			utils.NotFoundResponse(c, "Reported user")
			return
		}
	}

	// Rate limit per reporter.
	since := time.Now().Add(-time.Hour)
	if count, err := database.CountReportsByReporterSince(reporterID, since); err == nil && count >= maxReportsPerHour {
		utils.ErrorResponse(c, http.StatusTooManyRequests, "too many reports submitted; please try again later")
		return
	}

	report := &types.Report{
		ReporterID:     reporterID,
		Type:           req.Type,
		Reason:         req.Reason,
		Note:           req.Note,
		Status:         types.ReportStatusOpen,
		ReportedUserID: req.ReportedUserID,
		ThreadID:       req.ThreadID,
		MessageID:      req.MessageID,
		OutputType:     req.OutputType,
		OutputID:       req.OutputID,
		RoadmapID:      req.RoadmapID,
		JobID:          req.JobID,
		Context:        req.Context,
		AppVersion:     req.AppVersion,
		OS:             req.OS,
		Device:         req.Device,
	}

	if err := database.CreateReport(ctx, report); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusCreated, report)
	log.Info(ctx, "report created: "+report.ReportID)

	// Notify support out-of-band. Use a fresh context: the request context is
	// cancelled the moment we return the 201 above. A send failure is logged,
	// not fatal - the persisted row is the source of truth.
	go h.dispatchReportEmail(*report)
}

// dispatchReportEmail renders and sends the support notification for a report.
func (h *ReportHandler) dispatchReportEmail(report types.Report) {
	ctx := log.WithLogger(context.Background(), log.NewLogger(report.ReporterID))

	to := supportInbox()

	vars := email.Vars{
		ReportID:        report.ReportID,
		ReportType:      string(report.Type),
		ReportReason:    report.Reason,
		ReporterID:      report.ReporterID,
		ReportCreatedAt: report.CreatedAt.UTC().Format(time.RFC1123),
	}
	if report.Note != nil {
		vars.ReportNote = *report.Note
	}
	if u, err := database.GetUserByID(report.ReporterID); err == nil {
		vars.ReporterName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		vars.ReporterEmail = u.Email
	}
	if report.ReportedUserID != nil {
		vars.ReportedUserID = *report.ReportedUserID
		if u, err := database.GetUserByID(*report.ReportedUserID); err == nil {
			vars.ReportedUserName = strings.TrimSpace(u.FirstName + " " + u.LastName)
		}
	}
	if report.ThreadID != nil {
		vars.ReportThreadID = *report.ThreadID
	}
	if report.MessageID != nil {
		vars.ReportMessageID = *report.MessageID
	}
	if report.OutputType != nil {
		vars.ReportOutputType = *report.OutputType
	}
	if report.OutputID != nil {
		vars.ReportOutputID = *report.OutputID
	}
	if report.RoadmapID != nil {
		vars.ReportRoadmapID = *report.RoadmapID
	}
	if report.JobID != nil {
		vars.ReportJobID = *report.JobID
	}
	if len(report.Context) > 0 {
		if b, err := json.MarshalIndent(report.Context, "", "  "); err == nil {
			vars.ReportContext = string(b)
		}
	}

	// dedupeKey = report_id so a client retry never double-notifies support.
	// Reply-To is the reporter (when we resolved one) so the team can respond
	// from their mail client without digging the address out of the body.
	if err := h.mailer.SendToAddressWithReplyTo(ctx, to, vars.ReporterEmail, report.ReporterID, email.KindReportReceived, vars, report.ReportID); err != nil {
		log.Error(ctx, "failed to send report email", err)
	}
}
