package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CreateBackgroundJob(c *gin.Context, job *types.BackgroundJob) error {

	return db.Create(job).Error
}

func GetBackgroundJob(c *gin.Context, jobID string, userID string) (*types.BackgroundJob, error) {
	var job types.BackgroundJob
	err := db.Where("job_id = ? AND user_id = ?", jobID, userID).First(&job).Error

	return &job, err
}

// StaleJobError is the error text stamped on jobs the reaper times out. Also
// surfaced to the client via GET /jobs/:jobid/status, so keep it user-legible.
const StaleJobError = "Generation timed out and was not completed. Please try again."

// StaleJob is a background job the reaper failed, paired with the draft
// roadmap that was left mid-generation behind it. RoadmapID is empty for job
// types that don't create a draft roadmap.
type StaleJob struct {
	JobID     string
	UserID    string
	Type      string
	RoadmapID string
}

// FailStaleJobs fails every background job left in pending or processing with
// no progress update since cutoff, and flips any roadmap still pointing at
// those jobs to generation_status='failed'.
//
// This is the recovery path for generation goroutines that die without
// running their own error handling — most commonly a deploy or a crash, since
// processRoadmapGeneration and friends run detached from the request and are
// not tracked by the server's graceful shutdown. Without this, the job row
// stays 'processing' forever and the user is stuck on a roadmap that never
// finishes and can't be retried.
//
// The UPDATE claims rows atomically and returns what it claimed, so two
// machines running this concurrently can't both reap — and therefore can't
// both notify — the same job.
func FailStaleJobs(cutoff time.Time) ([]StaleJob, error) {
	now := time.Now()

	var claimed []types.BackgroundJob
	res := db.Model(&claimed).
		Clauses(clause.Returning{}).
		Where("status IN ? AND updated_at < ?",
			[]string{string(types.JobStatusPending), string(types.JobStatusProcessing)},
			cutoff).
		Updates(map[string]any{
			"status":       string(types.JobStatusFailed),
			"error":        StaleJobError,
			"completed_at": now,
			"updated_at":   now,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if len(claimed) == 0 {
		return nil, nil
	}

	jobIDs := make([]string, len(claimed))
	for i, j := range claimed {
		jobIDs[i] = j.JobID
	}

	// Resolve the draft roadmaps before flipping them, so the caller can tell
	// the user which roadmap died rather than just that "a job" failed.
	type roadmapRef struct {
		RoadmapID string
		JobID     string
	}
	var refs []roadmapRef
	if err := db.Model(&types.Roadmap{}).
		Select("roadmap_id", "job_id").
		Where("job_id IN ?", jobIDs).
		Scan(&refs).Error; err != nil {
		return nil, err
	}

	byJob := make(map[string]string, len(refs))
	for _, r := range refs {
		byJob[r.JobID] = r.RoadmapID
	}

	// Only move roadmaps that are still mid-generation. A roadmap that already
	// reached 'completed' belongs to a job that finished its work but died
	// before marking itself done — failing it would destroy a good roadmap.
	if err := db.Model(&types.Roadmap{}).
		Where("job_id IN ? AND generation_status = ?",
			jobIDs, string(types.GenerationStatusProcessing)).
		Update("generation_status", string(types.GenerationStatusFailed)).Error; err != nil {
		return nil, err
	}

	stale := make([]StaleJob, len(claimed))
	for i, j := range claimed {
		stale[i] = StaleJob{
			JobID:     j.JobID,
			UserID:    j.UserID,
			Type:      j.Type,
			RoadmapID: byJob[j.JobID],
		}
	}
	return stale, nil
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
