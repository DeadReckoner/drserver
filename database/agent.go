package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func CreateBackgroundJob(c *gin.Context, job *types.BackgroundJob) error {

	return db.Create(job).Error
}

func GetBackgroundJob(c *gin.Context, jobID string, userID string) (*types.BackgroundJob, error) {
	var job types.BackgroundJob
	err := db.Where("job_id = ? AND user_id = ?", jobID, userID).First(&job).Error

	return &job, err
}

func UpdateBackgroundJob(ctx context.Context, job *types.BackgroundJob) error {
	updates := map[string]any{}
	if job.Progress > 0 {
		updates["progress"] = job.Progress
	}
	if job.Status != "" {
		updates["status"] = job.Status
	}
	if job.Error != "" {
		updates["error"] = job.Error
	}
	if job.StartedAt != nil {
		updates["started_at"] = job.StartedAt
	}
	if job.CompletedAt != nil {
		updates["completed_at"] = job.CompletedAt
	}
	if job.Result != nil {
		// Manually serialize to JSON for JSONB column since Updates() doesn't use serializer tags
		resultJSON, err := json.Marshal(job.Result)
		if err != nil {
			return fmt.Errorf("failed to marshal result: %w", err)
		}
		// Use gorm.Expr to tell PostgreSQL to cast the JSON string to JSONB type
		updates["result"] = gorm.Expr("?::jsonb", string(resultJSON))
	}
	updates["updated_at"] = time.Now()
	return db.Model(&types.BackgroundJob{}).Where("job_id = ?", job.JobID).Updates(updates).Error
}
