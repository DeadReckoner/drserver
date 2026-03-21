package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/types"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type FeedbackHandler struct {
	db *gorm.DB
}

func NewFeedbackHandler(db *gorm.DB) *FeedbackHandler {
	return &FeedbackHandler{db: db}
}

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
}
