package database

import (
	"context"
	"errors"
	"hash/fnv"
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
	return findJobByIdempotencyKeyTx(db, userID, jobType, key)
}

func findJobByIdempotencyKeyTx(tx *gorm.DB, userID string, jobType types.JobType, key string) (*types.BackgroundJob, error) {
	if userID == "" || key == "" {
		return nil, nil
	}

	var job types.BackgroundJob
	err := tx.Where("user_id = ? AND type = ? AND payload->>'idempotency_key' = ?", userID, string(jobType), key).
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
	return countActiveJobsOfTypeTx(db, userID, jobType, time.Now().Add(-staleAfter))
}

func countActiveJobsOfTypeTx(tx *gorm.DB, userID string, jobType types.JobType, notBefore time.Time) (int64, error) {
	var count int64
	err := tx.Model(&types.BackgroundJob{}).
		Where("user_id = ? AND type = ? AND status IN ? AND created_at > ?",
			userID, string(jobType),
			[]string{string(types.JobStatusPending), string(types.JobStatusProcessing)},
			notBefore).
		Count(&count).Error
	return count, err
}

// LatestJobCreatedAt returns when the user last started a job of this type, or
// the zero time when they never have. Backs the per-user cooldown that bounds
// how fast a client can start distinct generations.
func LatestJobCreatedAt(userID string, jobType types.JobType) (time.Time, error) {
	return latestJobCreatedAtTx(db, userID, jobType)
}

func latestJobCreatedAtTx(tx *gorm.DB, userID string, jobType types.JobType) (time.Time, error) {
	var job types.BackgroundJob
	err := tx.Select("created_at").
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

// ─── atomic admission ────────────────────────────────────────────────────────

// goalDiscoveryLockNamespace is the first key of the transaction-scoped
// advisory lock. A fixed namespace keeps goal-discovery locks from colliding
// with any other advisory lock this codebase might take later; the second key
// is derived from the user id, so two different users never serialise on each
// other.
const goalDiscoveryLockNamespace int32 = 0x60A1D15C

// advisoryKeyForUser maps a user id onto the int32 second half of the advisory
// lock key. FNV-1a rather than the database's hashtext() so the value is
// computed identically regardless of driver, collation, or server version.
//
// A hash collision between two users is harmless: the worst outcome is that two
// unrelated users briefly serialise on the same lock.
func advisoryKeyForUser(userID string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID))
	return int32(h.Sum32()) // #nosec G115 -- deliberate wrap into the int4 lock key space
}

// Rejection codes returned by AdmitGoalOptionsJob. They are surfaced to the
// client verbatim as the `code` field, so the app can branch on them.
const (
	RejectGoalOptionsInProgress    = "generation_in_progress"
	RejectGoalOptionsRateLimited   = "rate_limited"
	RejectGoalOptionsDailyLimit    = "goal_options_daily_limit"
	RejectGoalOptionsDraftExceeded = "goal_options_regeneration_limit"
)

// GoalOptionsAdmission is everything the admission decision needs. Every bound
// is passed in rather than read from a package global so the caller owns policy
// and the decision is deterministically testable with an injected clock.
type GoalOptionsAdmission struct {
	UserID         string
	IdempotencyKey string
	// ChainID groups the generations of one discovery draft. Empty for a first
	// generation (there is nothing to cap yet); set to the inherited chain on
	// every regeneration.
	ChainID    string
	Now        time.Time
	StaleAfter time.Duration
	Cooldown   time.Duration
	// DailyLimit bounds option generations per user per UTC day; DraftLimit
	// bounds them per discovery draft. A negative value means unlimited.
	DailyLimit int
	DraftLimit int
}

// GoalOptionsAdmissionResult reports what happened. Exactly one of Existing,
// Created, or Rejection is meaningful.
type GoalOptionsAdmissionResult struct {
	// Existing is the job a matching idempotency key already created.
	Existing *types.BackgroundJob
	Created  bool
	// Rejection is one of the Reject* codes, or "" when admitted.
	Rejection string
	// Used/Limit describe the bound that rejected the request, for the message.
	Used  int
	Limit int
	// RetryAfter is set for RejectGoalOptionsRateLimited.
	RetryAfter time.Duration
	// Expired counts jobs this call transitioned out of a stuck pending/
	// processing state. Reported so the caller can log a recovery.
	Expired int64
}

// AdmitGoalOptionsJob makes the whole admission decision and the insert inside
// ONE transaction held under a per-user advisory lock.
//
// Why a lock rather than check-then-insert: every guard here (idempotency,
// one-in-flight, cooldown, daily and per-draft caps) is a read followed by a
// write. Two requests racing between the read and the write both see "allowed"
// and both insert — which is exactly how a double-tapped button, a retried
// request after a dropped response, or a malicious client bypasses all five
// bounds at once. pg_advisory_xact_lock serialises the whole decision per user
// and is released automatically when the transaction ends, including on
// rollback or a dropped connection, so a crash mid-decision cannot wedge the
// lock.
//
// The lock is taken on the USER, so unrelated users never contend.
func AdmitGoalOptionsJob(job *types.BackgroundJob, in GoalOptionsAdmission) (*GoalOptionsAdmissionResult, error) {
	if in.UserID == "" {
		return nil, errors.New("missing user_id")
	}
	if in.Now.IsZero() {
		in.Now = time.Now()
	}

	result := &GoalOptionsAdmissionResult{}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)",
			goalDiscoveryLockNamespace, advisoryKeyForUser(in.UserID)).Error; err != nil {
			return err
		}

		// Retire abandoned jobs FIRST, so every guard below sees the same
		// post-expiry view of the world that GET /jobs/:jobid/status does.
		expired, err := expireStaleJobsTx(tx, in.UserID, types.JobTypeGoalDiscovery,
			in.Now.Add(-in.StaleAfter), in.Now)
		if err != nil {
			return err
		}
		result.Expired = expired

		// Idempotent repeat wins over every other guard: returning the job the
		// key already created is what makes a retry after a dropped response
		// safe, and it must not be refused by the cooldown or the caps.
		existing, err := findJobByIdempotencyKeyTx(tx, in.UserID, types.JobTypeGoalDiscovery, in.IdempotencyKey)
		if err != nil {
			return err
		}
		if existing != nil {
			result.Existing = existing
			return nil
		}

		active, err := countActiveJobsOfTypeTx(tx, in.UserID, types.JobTypeGoalDiscovery, in.Now.Add(-in.StaleAfter))
		if err != nil {
			return err
		}
		if active > 0 {
			result.Rejection = RejectGoalOptionsInProgress
			return nil
		}

		if in.Cooldown > 0 {
			lastAt, err := latestJobCreatedAtTx(tx, in.UserID, types.JobTypeGoalDiscovery)
			if err != nil {
				return err
			}
			if !lastAt.IsZero() {
				if elapsed := in.Now.Sub(lastAt); elapsed < in.Cooldown {
					result.Rejection = RejectGoalOptionsRateLimited
					result.RetryAfter = in.Cooldown - elapsed
					return nil
				}
			}
		}

		if in.DailyLimit >= 0 {
			used, err := countJobsSinceTx(tx, in.UserID, types.JobTypeGoalDiscovery, startOfUTCDay(in.Now))
			if err != nil {
				return err
			}
			if int(used) >= in.DailyLimit {
				result.Rejection = RejectGoalOptionsDailyLimit
				result.Used, result.Limit = int(used), in.DailyLimit
				return nil
			}
		}

		if in.DraftLimit >= 0 && in.ChainID != "" {
			used, err := countJobsInChainTx(tx, in.UserID, in.ChainID)
			if err != nil {
				return err
			}
			if int(used) >= in.DraftLimit {
				result.Rejection = RejectGoalOptionsDraftExceeded
				result.Used, result.Limit = int(used), in.DraftLimit
				return nil
			}
		}

		if err := tx.Create(job).Error; err != nil {
			return err
		}
		result.Created = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// startOfUTCDay matches the entitlement counter's day boundary
// (shouldResetAIPeriod), so "today" means the same thing to both budgets.
func startOfUTCDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func countJobsSinceTx(tx *gorm.DB, userID string, jobType types.JobType, since time.Time) (int64, error) {
	var count int64
	err := tx.Model(&types.BackgroundJob{}).
		Where("user_id = ? AND type = ? AND created_at >= ?", userID, string(jobType), since).
		Count(&count).Error
	return count, err
}

func countJobsInChainTx(tx *gorm.DB, userID, chainID string) (int64, error) {
	var count int64
	err := tx.Model(&types.BackgroundJob{}).
		Where("user_id = ? AND type = ? AND payload->>'chain_id' = ?",
			userID, string(types.JobTypeGoalDiscovery), chainID).
		Count(&count).Error
	return count, err
}

// CountGoalOptionsJobsToday reports how many option generations the user has
// started today (UTC). Exposed so the handler can report remaining budget.
func CountGoalOptionsJobsToday(userID string, now time.Time) (int64, error) {
	return countJobsSinceTx(db, userID, types.JobTypeGoalDiscovery, startOfUTCDay(now))
}

// ─── stale jobs and late workers ─────────────────────────────────────────────

// ExpireStaleJobs retires every pending/processing job of a type for a user
// that was created at or before `cutoff`, marking it failed with the typed
// expiry reason.
//
// This is the recoverable terminal state the architecture needs: with no
// durable queue, a job whose goroutine died has nobody left to finish it, so
// something on a request path has to declare it over. Marking it failed (rather
// than deleting it) keeps the client's existing failure handling — which
// preserves the user's answers and offers retry — working unchanged.
func ExpireStaleJobs(userID string, jobType types.JobType, cutoff, now time.Time) (int64, error) {
	return expireStaleJobsTx(db, userID, jobType, cutoff, now)
}

func expireStaleJobsTx(tx *gorm.DB, userID string, jobType types.JobType, cutoff, now time.Time) (int64, error) {
	res := tx.Model(&types.BackgroundJob{}).
		Where("user_id = ? AND type = ? AND status IN ? AND created_at <= ?",
			userID, string(jobType),
			[]string{string(types.JobStatusPending), string(types.JobStatusProcessing)},
			cutoff).
		Updates(map[string]any{
			"status":       string(types.JobStatusFailed),
			"error":        types.JobExpiredError,
			"completed_at": now,
			"updated_at":   now,
		})
	return res.RowsAffected, res.Error
}

// ExpireJobIfStale retires ONE job when it is still unfinished past its type's
// window. Used by the status read so a client polling a dead job is told it is
// over instead of waiting forever, and so reads and writes apply the same rule.
//
// Returns true when this call is what retired the job.
func ExpireJobIfStale(job *types.BackgroundJob, now time.Time) (bool, error) {
	if job == nil {
		return false, nil
	}
	if job.Status != string(types.JobStatusPending) && job.Status != string(types.JobStatusProcessing) {
		return false, nil
	}
	cutoff := now.Add(-types.StaleAfterForJobType(types.JobType(job.Type)))
	if job.CreatedAt.After(cutoff) {
		return false, nil
	}

	res := db.Model(&types.BackgroundJob{}).
		Where("job_id = ? AND status IN ?", job.JobID,
			[]string{string(types.JobStatusPending), string(types.JobStatusProcessing)}).
		Updates(map[string]any{
			"status":       string(types.JobStatusFailed),
			"error":        types.JobExpiredError,
			"completed_at": now,
			"updated_at":   now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}

	// Reflect the transition in the caller's copy so the response it builds
	// describes the row that now exists rather than the one it read.
	job.Status = string(types.JobStatusFailed)
	job.Error = types.JobExpiredError
	completed := now
	job.CompletedAt = &completed
	return true, nil
}

// FinishJobIfActive is the compare-and-set a background worker must use to
// publish its result.
//
// A worker that was superseded — its job expired as stale, or the user started
// a replacement — must not be able to write a completed status, install its now
// obsolete option set over the newer one, or trigger a completion notification
// for work nobody is waiting on. Guarding the UPDATE on the job still being
// pending/processing turns "did I win?" into a single atomic question, and the
// caller does nothing user-visible when the answer is no.
//
// Returns false (with no error) when the job had already reached a terminal
// state.
func FinishJobIfActive(ctx context.Context, job *types.BackgroundJob) (bool, error) {
	if job == nil || job.JobID == "" {
		return false, errors.New("missing job_id")
	}

	updates := map[string]any{"updated_at": time.Now()}
	if job.Progress > 0 {
		updates["progress"] = job.Progress
	}
	if job.Status != "" {
		updates["status"] = job.Status
	}
	if job.Error != "" {
		updates["error"] = job.Error
	}
	if job.CompletedAt != nil {
		updates["completed_at"] = job.CompletedAt
	}
	if job.Result != nil {
		resultJSON, err := marshalJSONB(job.Result)
		if err != nil {
			return false, err
		}
		updates["result"] = resultJSON
	}

	res := db.WithContext(ctx).Model(&types.BackgroundJob{}).
		Where("job_id = ? AND status IN ?", job.JobID,
			[]string{string(types.JobStatusPending), string(types.JobStatusProcessing)}).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
