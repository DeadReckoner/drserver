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
		resultJSON, err := marshalJSONB(job.Result)
		if err != nil {
			return err
		}
		updates["result"] = resultJSON
	}
	updates["updated_at"] = time.Now()
	return db.Model(&types.BackgroundJob{}).Where("job_id = ?", job.JobID).Updates(updates).Error
}

// marshalJSONB prepares a value for a JSONB column in an Updates() map.
// Updates() bypasses GORM's serializer tags, so the value is marshalled by hand
// and wrapped in an explicit ::jsonb cast for PostgreSQL.
func marshalJSONB(v any) (any, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}
	return gorm.Expr("?::jsonb", string(encoded)), nil
}
