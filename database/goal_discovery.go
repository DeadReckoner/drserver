package database

import (
	"errors"
	"time"

	"dr/server/types"

	"gorm.io/gorm"
)

// Goal-discovery job queries. These all run against the existing background_jobs
// table — the feature adds a job TYPE, not a new store.

// FindJobByIdempotencyKey returns the caller's existing job for an idempotency
// key, or nil when there is none. The key lives inside the JSONB payload, so no
// column was added for it.
//
// Scoped by user_id as well as key: one user's key must never resolve to
// another user's job, even if a client generates a colliding value.
func FindJobByIdempotencyKey(userID string, jobType types.JobType, key string) (*types.BackgroundJob, error) {
	if userID == "" || key == "" {
		return nil, nil
	}

	var job types.BackgroundJob
	err := db.Where("user_id = ? AND type = ? AND payload->>'idempotency_key' = ?", userID, string(jobType), key).
		Order("created_at DESC").
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// CountActiveJobsOfType counts the user's jobs of one type that are still
// running. Used to refuse a second concurrent generation rather than paying for
// two models to answer the same question.
//
// Only jobs newer than `staleAfter` count. A job whose worker died — a crash, a
// deploy mid-generation — stays 'processing' forever, and without this bound
// that one orphan would lock the user out of the feature permanently. The window
// must stay comfortably longer than a real generation so it never races a job
// that is genuinely still working.
func CountActiveJobsOfType(userID string, jobType types.JobType, staleAfter time.Duration) (int64, error) {
	var count int64
	err := db.Model(&types.BackgroundJob{}).
		Where("user_id = ? AND type = ? AND status IN ? AND created_at > ?",
			userID, string(jobType),
			[]string{string(types.JobStatusPending), string(types.JobStatusProcessing)},
			time.Now().Add(-staleAfter)).
		Count(&count).Error
	return count, err
}

// LatestJobCreatedAt returns when the user last started a job of this type, or
// the zero time when they never have. Backs the per-user cooldown that bounds
// how fast a client can start distinct generations.
func LatestJobCreatedAt(userID string, jobType types.JobType) (time.Time, error) {
	var job types.BackgroundJob
	err := db.Select("created_at").
		Where("user_id = ? AND type = ?", userID, string(jobType)).
		Order("created_at DESC").
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return job.CreatedAt, nil
}

// GetGoalDiscoveryJob loads one goal_discovery job belonging to the user.
// Filtering on type as well as id stops a client from passing an unrelated job
// id (say, a roadmap job) and having it treated as a discovery result.
func GetGoalDiscoveryJob(userID, jobID string) (*types.BackgroundJob, error) {
	var job types.BackgroundJob
	err := db.Where("job_id = ? AND user_id = ? AND type = ?", jobID, userID, string(types.JobTypeGoalDiscovery)).
		First(&job).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// CreateJob inserts a background job. Mirrors CreateBackgroundJob but without
// the unused *gin.Context parameter, so background paths can create jobs too.
func CreateJob(job *types.BackgroundJob) error {
	return db.Create(job).Error
}
