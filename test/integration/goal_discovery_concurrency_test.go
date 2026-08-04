//go:build integration

package integration

// Concurrency and lifecycle guarantees for goal-discovery job admission, run
// against REAL PostgreSQL.
//
// These cannot be proved with sqlmock or a mocked repository. Every property
// here is a race: two transactions interleaving between a read and a write. A
// mock replays a scripted statement order, which is precisely the thing under
// test, so it would report success no matter how the locking was written.
//
// The container is throwaway (see main_test.go); nothing here touches a local
// or hosted database.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"dr/server/database"
	"dr/server/types"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gdcAdmission builds the admission policy used by most cases: generous bounds
// so a case is only ever rejected by the guard it is actually testing.
func gdcAdmission(userID string, now time.Time) database.GoalOptionsAdmission {
	return database.GoalOptionsAdmission{
		UserID:     userID,
		Now:        now,
		StaleAfter: 15 * time.Minute,
		Cooldown:   0,
		DailyLimit: -1,
		DraftLimit: -1,
	}
}

func gdcJob(userID string, payload types.GoalDiscoveryJobPayload) *types.BackgroundJob {
	return &types.BackgroundJob{
		JobID:    uuid.NewString(),
		UserID:   userID,
		Type:     string(types.JobTypeGoalDiscovery),
		Status:   string(types.JobStatusPending),
		Progress: 0,
		Payload:  payload,
	}
}

func gdcCountJobs(t *testing.T, userID string) int {
	t.Helper()
	return countRows(t, `select count(*) from public.background_jobs where user_id = $1`, userID)
}

// Two simultaneous requests carrying the SAME idempotency key must create
// exactly one job and both callers must be told about the same one.
//
// This is the double-tap and the retry-after-a-dropped-response case. With a
// check-then-insert both goroutines see "no existing job" and both insert.
func TestAdmitGoalOptionsJob_SameIdempotencyKeyCreatesExactlyOneJob(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()
	const key = "same-key"

	const racers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		jobIDs  = map[string]struct{}{}
		start   = make(chan struct{})
	)

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release everyone at once to maximise the interleaving

			job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: key, ChainID: "chain-a"})
			in := gdcAdmission(userID, now)
			in.IdempotencyKey = key

			res, err := database.AdmitGoalOptionsJob(job, in)
			if err != nil {
				t.Errorf("admission failed: %v", err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			switch {
			case res.Created:
				created++
				jobIDs[job.JobID] = struct{}{}
			case res.Existing != nil:
				jobIDs[res.Existing.JobID] = struct{}{}
			default:
				// An idempotent repeat must never be turned away by the
				// in-flight guard: the caller is asking about the job that
				// guard is protecting.
				t.Errorf("idempotent repeat was rejected with %q", res.Rejection)
			}
		}()
	}

	close(start)
	wg.Wait()

	assert.Equal(t, 1, created, "exactly one racer may create the job")
	assert.Equal(t, 1, gdcCountJobs(t, userID), "exactly one row may exist")
	assert.Len(t, jobIDs, 1, "every caller must be told about the SAME job")
}

// Two simultaneous requests with DIFFERENT idempotency keys must not both
// create an active job for one user. One wins; the other is told a generation
// is already running.
func TestAdmitGoalOptionsJob_DifferentKeysCannotBothCreateActiveJobs(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	const racers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		created  int
		rejected int
		start    = make(chan struct{})
	)

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start

			key := fmt.Sprintf("distinct-key-%d", n)
			job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: key, ChainID: "chain-b"})
			in := gdcAdmission(userID, now)
			in.IdempotencyKey = key

			res, err := database.AdmitGoalOptionsJob(job, in)
			if err != nil {
				t.Errorf("admission failed: %v", err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			if res.Created {
				created++
				return
			}
			require.Equal(t, database.RejectGoalOptionsInProgress, res.Rejection)
			rejected++
		}(i)
	}

	close(start)
	wg.Wait()

	assert.Equal(t, 1, created, "only one concurrent generation may start")
	assert.Equal(t, racers-1, rejected)
	assert.Equal(t, 1, gdcCountJobs(t, userID))
}

// The per-user lock must not serialise UNRELATED users: two accounts racing
// each other both get their job.
func TestAdmitGoalOptionsJob_LockIsScopedToOneUser(t *testing.T) {
	userA := createUser(t, "UTC")
	userB := createUser(t, "UTC")
	now := time.Now()

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]bool, 2)

	for i, userID := range []string{userA, userB} {
		wg.Add(1)
		go func(idx int, uid string) {
			defer wg.Done()
			<-start
			job := gdcJob(uid, types.GoalDiscoveryJobPayload{IdempotencyKey: "k", ChainID: "c"})
			in := gdcAdmission(uid, now)
			in.IdempotencyKey = "k"
			res, err := database.AdmitGoalOptionsJob(job, in)
			require.NoError(t, err)
			results[idx] = res.Created
		}(i, userID)
	}

	close(start)
	wg.Wait()

	assert.True(t, results[0] && results[1], "two different users must not block each other")
	assert.Equal(t, 1, gdcCountJobs(t, userA))
	assert.Equal(t, 1, gdcCountJobs(t, userB))
}

// Racing the per-draft regeneration cap must not let extra jobs through: the
// count and the insert are inside the same locked transaction.
func TestAdmitGoalOptionsJob_DraftCapHoldsUnderConcurrency(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()
	const chain = "capped-chain"

	// Seed the chain right up to its cap, all terminal so the in-flight guard
	// is not what does the rejecting.
	for i := 0; i < types.GoalOptionsPerDraftLimit; i++ {
		job := gdcJob(userID, types.GoalDiscoveryJobPayload{
			IdempotencyKey: fmt.Sprintf("seed-%d", i), ChainID: chain,
		})
		job.Status = string(types.JobStatusCompleted)
		require.NoError(t, database.CreateJob(job))
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		created  int
		rejected int
		start    = make(chan struct{})
	)

	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start

			key := fmt.Sprintf("race-%d", n)
			job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: key, ChainID: chain})
			in := gdcAdmission(userID, now)
			in.IdempotencyKey = key
			in.ChainID = chain
			in.DraftLimit = types.GoalOptionsPerDraftLimit

			res, err := database.AdmitGoalOptionsJob(job, in)
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()
			if res.Created {
				created++
				return
			}
			assert.Equal(t, database.RejectGoalOptionsDraftExceeded, res.Rejection)
			rejected++
		}(i)
	}

	close(start)
	wg.Wait()

	assert.Zero(t, created, "the cap must hold even when every request arrives at once")
	assert.Equal(t, 6, rejected)
	assert.Equal(t, types.GoalOptionsPerDraftLimit, gdcCountJobs(t, userID))
}

// ─── stale jobs and late workers ─────────────────────────────────────────────

// A job whose worker died must not lock the user out forever. Admission retires
// it to a TYPED terminal state and then lets the new request through.
func TestAdmitGoalOptionsJob_ExpiresAbandonedJobAndAdmitsTheNextOne(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	orphan := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "orphan", ChainID: "chain-c"})
	orphan.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(orphan))
	// Backdate it past the stale window. The clock is injected rather than
	// slept on, so the threshold is exercised deterministically.
	require.NoError(t, testDB.Exec(
		`update public.background_jobs set created_at = $1 where job_id = $2`,
		now.Add(-2*time.Hour), orphan.JobID).Error)

	job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "fresh", ChainID: "chain-c"})
	in := gdcAdmission(userID, now)
	in.IdempotencyKey = "fresh"

	res, err := database.AdmitGoalOptionsJob(job, in)
	require.NoError(t, err)

	assert.True(t, res.Created, "an abandoned job must not block a new one")
	assert.EqualValues(t, 1, res.Expired)

	var status, jobErr string
	require.NoError(t, testDB.Raw(
		`select status, coalesce(error, '') from public.background_jobs where job_id = $1`,
		orphan.JobID).Row().Scan(&status, &jobErr))
	assert.Equal(t, string(types.JobStatusFailed), status,
		"a stale job must reach a recoverable TERMINAL state, not stay processing")
	assert.Equal(t, types.JobExpiredError, jobErr,
		"the reason must be typed so the client can tell it from a generation failure")
}

// A job still inside its window is genuinely running and must be left alone.
func TestAdmitGoalOptionsJob_RecentInFlightJobIsNotExpired(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	running := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "running", ChainID: "chain-d"})
	running.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(running))

	job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "second", ChainID: "chain-d"})
	in := gdcAdmission(userID, now)
	in.IdempotencyKey = "second"

	res, err := database.AdmitGoalOptionsJob(job, in)
	require.NoError(t, err)

	assert.False(t, res.Created)
	assert.Zero(t, res.Expired)
	assert.Equal(t, database.RejectGoalOptionsInProgress, res.Rejection)

	var status string
	require.NoError(t, testDB.Raw(
		`select status from public.background_jobs where job_id = $1`, running.JobID).Row().Scan(&status))
	assert.Equal(t, string(types.JobStatusProcessing), status)
}

// The status read applies the SAME rule as job creation, so a client polling a
// dead job is told it is over instead of waiting forever.
func TestExpireJobIfStale_StatusReadRetiresAbandonedJob(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	orphan := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "poll-orphan", ChainID: "chain-e"})
	orphan.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(orphan))
	require.NoError(t, testDB.Exec(
		`update public.background_jobs set created_at = $1 where job_id = $2`,
		now.Add(-2*time.Hour), orphan.JobID).Error)

	loaded := &types.BackgroundJob{
		JobID:     orphan.JobID,
		Type:      string(types.JobTypeGoalDiscovery),
		Status:    string(types.JobStatusProcessing),
		CreatedAt: now.Add(-2 * time.Hour),
	}

	expired, err := database.ExpireJobIfStale(loaded, now)
	require.NoError(t, err)
	assert.True(t, expired)
	// The caller's copy is updated so the response it builds describes the row
	// that now exists.
	assert.Equal(t, string(types.JobStatusFailed), loaded.Status)
	assert.Equal(t, types.JobExpiredError, loaded.Error)

	// Idempotent: a second poll does not re-expire or re-notify.
	again, err := database.ExpireJobIfStale(loaded, now)
	require.NoError(t, err)
	assert.False(t, again)
}

func TestExpireJobIfStale_LeavesRecentJobAlone(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	running := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "poll-live", ChainID: "chain-f"})
	running.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(running))

	loaded := &types.BackgroundJob{
		JobID:     running.JobID,
		Type:      string(types.JobTypeGoalDiscovery),
		Status:    string(types.JobStatusProcessing),
		CreatedAt: now,
	}
	expired, err := database.ExpireJobIfStale(loaded, now)
	require.NoError(t, err)
	assert.False(t, expired)
	assert.Equal(t, string(types.JobStatusProcessing), loaded.Status)
}

// A worker that comes back after its job was retired must lose. It must not
// resurrect the row as completed, and — because FinishJobIfActive reports the
// loss — its caller must not emit a completion notification either.
func TestFinishJobIfActive_LateWorkerCannotOverwriteATerminalJob(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()
	ctx := context.Background()

	job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "late", ChainID: "chain-g"})
	job.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(job))
	require.NoError(t, testDB.Exec(
		`update public.background_jobs set created_at = $1 where job_id = $2`,
		now.Add(-2*time.Hour), job.JobID).Error)

	// Something else declares the job over while the worker is still running.
	expired, err := database.ExpireStaleJobs(userID, types.JobTypeGoalDiscovery, now.Add(-15*time.Minute), now)
	require.NoError(t, err)
	require.EqualValues(t, 1, expired)

	// The late worker now tries to publish its result.
	completedAt := now
	won, err := database.FinishJobIfActive(ctx, &types.BackgroundJob{
		JobID:       job.JobID,
		Status:      string(types.JobStatusCompleted),
		Progress:    100,
		CompletedAt: &completedAt,
		Result: types.GoalOptionsResult{
			PromptVersion: types.GoalOptionsPromptVersion,
			GeneratedAt:   now.UTC(),
		},
	})
	require.NoError(t, err)
	assert.False(t, won, "a superseded worker must not win the compare-and-set")

	var status, jobErr string
	var result *string
	require.NoError(t, testDB.Raw(
		`select status, coalesce(error, ''), result::text from public.background_jobs where job_id = $1`,
		job.JobID).Row().Scan(&status, &jobErr, &result))

	assert.Equal(t, string(types.JobStatusFailed), status, "the terminal state must stand")
	assert.Equal(t, types.JobExpiredError, jobErr)
	assert.Nil(t, result, "a late worker must not install its obsolete option set")
}

// The same CAS on the happy path: an in-flight job's worker wins and its result
// lands.
func TestFinishJobIfActive_ActiveWorkerWinsAndWritesItsResult(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "winner", ChainID: "chain-h"})
	job.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(job))

	completedAt := now
	won, err := database.FinishJobIfActive(context.Background(), &types.BackgroundJob{
		JobID:       job.JobID,
		Status:      string(types.JobStatusCompleted),
		Progress:    100,
		CompletedAt: &completedAt,
		Result:      types.GoalOptionsResult{PromptVersion: types.GoalOptionsPromptVersion},
	})
	require.NoError(t, err)
	assert.True(t, won)

	var status string
	var result *string
	require.NoError(t, testDB.Raw(
		`select status, result::text from public.background_jobs where job_id = $1`,
		job.JobID).Row().Scan(&status, &result))
	assert.Equal(t, string(types.JobStatusCompleted), status)
	require.NotNil(t, result)
	assert.Contains(t, *result, types.GoalOptionsPromptVersion)
}

// Only ONE of two racing finishers may win, so a retried worker cannot double
// notify.
func TestFinishJobIfActive_OnlyOneOfTwoRacingFinishersWins(t *testing.T) {
	userID := createUser(t, "UTC")
	now := time.Now()

	job := gdcJob(userID, types.GoalDiscoveryJobPayload{IdempotencyKey: "race-finish", ChainID: "chain-i"})
	job.Status = string(types.JobStatusProcessing)
	require.NoError(t, database.CreateJob(job))

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
		start = make(chan struct{})
	)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			completedAt := now
			won, err := database.FinishJobIfActive(context.Background(), &types.BackgroundJob{
				JobID:       job.JobID,
				Status:      string(types.JobStatusCompleted),
				Progress:    100,
				CompletedAt: &completedAt,
				Result:      types.GoalOptionsResult{PromptVersion: types.GoalOptionsPromptVersion},
			})
			require.NoError(t, err)
			if won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, 1, wins, "exactly one finisher may be told it won, so exactly one notification is sent")
}

// ─── retention ───────────────────────────────────────────────────────────────

// Goal discovery answers are free text about health, money, and relationships.
// The retention story for them is the users foreign key, so it is worth an
// explicit test rather than an assumption about DDL.
func TestBackgroundJobs_AreDeletedWithTheAccount(t *testing.T) {
	userID := createUser(t, "UTC")

	job := gdcJob(userID, types.GoalDiscoveryJobPayload{
		IdempotencyKey: "retention",
		ChainID:        "chain-j",
		Answers:        types.GoalDiscoveryAnswers{RecurringThought: "sensitive free text"},
	})
	require.NoError(t, database.CreateJob(job))
	require.Equal(t, 1, gdcCountJobs(t, userID))

	require.NoError(t, database.DeleteUser(userID))

	assert.Zero(t, gdcCountJobs(t, userID),
		"account deletion must remove the stored answers, not orphan them")
}
