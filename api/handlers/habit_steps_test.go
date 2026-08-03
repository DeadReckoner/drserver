package handlers_test

// HabitHandler coverage for the multi-step recurring habit/task endpoints.
// The persistence semantics (cascade completion, idempotency, per-occurrence
// isolation, history immutability) are proven against real Postgres in
// test/integration/habit_steps_test.go; what is asserted here is the HTTP
// surface: routing, request binding, the status/completed dual, date parsing,
// and the mapping of domain errors onto status codes.

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	hsHabitID = uuid.NewString()
	hsStepID  = uuid.NewString()
)

// --- Template step definitions ------------------------------------------------

func TestHabitStep_Create_Success(t *testing.T) {
	var gotHabitID string
	var gotReq types.CreateHabitStepRequest
	svc := &testutil.FakeHabitService{
		CreateStepFunc: func(c *gin.Context, habitID string, req types.CreateHabitStepRequest) (types.HabitStep, error) {
			gotHabitID, gotReq = habitID, req
			return types.HabitStep{HabitStepID: hsStepID, HabitID: habitID, Description: req.Description, Number: 1}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits/"+hsHabitID+"/steps", testUser,
		types.CreateHabitStepRequest{Description: "Boil water"})

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
	}
	if gotHabitID != hsHabitID {
		t.Errorf("service got habitID=%q, want %q", gotHabitID, hsHabitID)
	}
	if gotReq.Description != "Boil water" {
		t.Errorf("service got description=%q, want %q", gotReq.Description, "Boil water")
	}

	var got types.HabitStep
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.HabitStepID != hsStepID {
		t.Errorf("response step id=%q, want %q", got.HabitStepID, hsStepID)
	}
}

func TestHabitStep_Create_RequiresDescription(t *testing.T) {
	// Unstubbed service: if binding fails to reject, the fake panics loudly.
	router := newHabitRouter(&testutil.FakeHabitService{})

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits/"+hsHabitID+"/steps", testUser,
		types.CreateHabitStepRequest{})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHabitStep_Create_UnownedHabitIs404(t *testing.T) {
	svc := &testutil.FakeHabitService{
		CreateStepFunc: func(c *gin.Context, habitID string, req types.CreateHabitStepRequest) (types.HabitStep, error) {
			return types.HabitStep{}, types.ErrHabitNotFound
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits/"+hsHabitID+"/steps", testUser,
		types.CreateHabitStepRequest{Description: "x"})

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404 (an unowned habit must be indistinguishable from a missing one); body=%s",
			w.Code, w.Body.String())
	}
}

func TestHabitStep_Update_PassesOnlySuppliedFields(t *testing.T) {
	var gotReq types.UpdateHabitStepRequest
	svc := &testutil.FakeHabitService{
		UpdateStepFunc: func(c *gin.Context, habitID, stepID string, req types.UpdateHabitStepRequest) (types.HabitStep, error) {
			gotReq = req
			return types.HabitStep{HabitStepID: stepID, Description: "renamed"}, nil
		},
	}
	router := newHabitRouter(svc)

	desc := "renamed"
	w := testutil.Do(t, router, http.MethodPatch,
		"/api/v1/habits/"+hsHabitID+"/steps/"+hsStepID, testUser,
		types.UpdateHabitStepRequest{Description: &desc})

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotReq.Description == nil || *gotReq.Description != "renamed" {
		t.Errorf("description not passed through: %+v", gotReq.Description)
	}
	if gotReq.Number != nil {
		t.Error("Number must stay nil when omitted, so an edit cannot silently reorder")
	}
}

func TestHabitStep_Delete_Success(t *testing.T) {
	called := false
	svc := &testutil.FakeHabitService{
		DeleteStepFunc: func(c *gin.Context, habitID, stepID string) error {
			called = true
			return nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/habits/"+hsHabitID+"/steps/"+hsStepID, testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Error("service DeleteStep not called")
	}
}

func TestHabitStep_Delete_MissingStepIs404(t *testing.T) {
	svc := &testutil.FakeHabitService{
		DeleteStepFunc: func(c *gin.Context, habitID, stepID string) error {
			return types.ErrHabitStepNotFound
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/habits/"+hsHabitID+"/steps/"+hsStepID, testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestHabitStep_Reorder_PassesIDOrder(t *testing.T) {
	var gotIDs []string
	a, b := uuid.NewString(), uuid.NewString()
	svc := &testutil.FakeHabitService{
		ReorderStepsFunc: func(c *gin.Context, habitID string, stepIDs []string) ([]types.HabitStep, error) {
			gotIDs = stepIDs
			return []types.HabitStep{{HabitStepID: b, Number: 1}, {HabitStepID: a, Number: 2}}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/habits/"+hsHabitID+"/steps/reorder", testUser,
		types.ReorderHabitStepsRequest{StepIDs: []string{b, a}})

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(gotIDs) != 2 || gotIDs[0] != b || gotIDs[1] != a {
		t.Errorf("service got ids=%v, want [%s %s]", gotIDs, b, a)
	}
}

// --- Occurrence step state -----------------------------------------------------

func TestHabitStep_GetOccurrence_DefaultsToToday(t *testing.T) {
	var gotDate time.Time
	svc := &testutil.FakeHabitService{
		OccurrenceStepsFunc: func(c *gin.Context, habitID string, date time.Time) (types.HabitOccurrenceSteps, error) {
			gotDate = date
			return types.HabitOccurrenceSteps{HabitID: habitID, StepsTotal: 2, StepsCompleted: 1}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/"+hsHabitID+"/occurrence/steps", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if dayOf(gotDate) != dayOf(time.Now()) {
		t.Errorf("service got date=%s, want today (%s)", dayOf(gotDate), dayOf(time.Now()))
	}

	var got types.HabitOccurrenceSteps
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.StepsTotal != 2 || got.StepsCompleted != 1 {
		t.Errorf("progress=%d/%d, want 1/2", got.StepsCompleted, got.StepsTotal)
	}
}

func TestHabitStep_GetOccurrence_ExplicitDate(t *testing.T) {
	var gotDate time.Time
	svc := &testutil.FakeHabitService{
		OccurrenceStepsFunc: func(c *gin.Context, habitID string, date time.Time) (types.HabitOccurrenceSteps, error) {
			gotDate = date
			return types.HabitOccurrenceSteps{HabitID: habitID}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/"+hsHabitID+"/occurrence/steps?date=2026-03-04", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotDate.Format("2006-01-02") != "2026-03-04" {
		t.Errorf("service got date=%s, want 2026-03-04", gotDate.Format("2006-01-02"))
	}
}

func TestHabitStep_GetOccurrence_BadDateIs400(t *testing.T) {
	router := newHabitRouter(&testutil.FakeHabitService{})

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/"+hsHabitID+"/occurrence/steps?date=04-03-2026", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHabitStep_SetState_StatusAndCompletedBothAccepted(t *testing.T) {
	cases := []struct {
		name string
		body types.SetHabitStepStateRequest
		want string
	}{
		{"explicit completed status", types.SetHabitStepStateRequest{Status: strPtr("completed")}, "completed"},
		{"explicit skipped status", types.SetHabitStepStateRequest{Status: strPtr("skipped")}, "skipped"},
		{"explicit incomplete status", types.SetHabitStepStateRequest{Status: strPtr("incomplete")}, "incomplete"},
		{"legacy completed=true", types.SetHabitStepStateRequest{Completed: boolPtr(true)}, "completed"},
		{"legacy completed=false", types.SetHabitStepStateRequest{Completed: boolPtr(false)}, "incomplete"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotStatus string
			svc := &testutil.FakeHabitService{
				SetStepStateFunc: func(c *gin.Context, habitID, stepID string, date time.Time, status string) (types.HabitOccurrenceSteps, error) {
					gotStatus = status
					return types.HabitOccurrenceSteps{HabitID: habitID}, nil
				},
			}
			router := newHabitRouter(svc)

			w := testutil.Do(t, router, http.MethodPatch,
				"/api/v1/habits/"+hsHabitID+"/occurrence/steps/"+hsStepID, testUser, tc.body)

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
			}
			if gotStatus != tc.want {
				t.Errorf("service got status=%q, want %q", gotStatus, tc.want)
			}
		})
	}
}

func TestHabitStep_SetState_InvalidInputIs400(t *testing.T) {
	cases := []struct {
		name string
		body types.SetHabitStepStateRequest
	}{
		{"unknown status", types.SetHabitStepStateRequest{Status: strPtr("almost")}},
		{"neither status nor completed", types.SetHabitStepStateRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newHabitRouter(&testutil.FakeHabitService{})

			w := testutil.Do(t, router, http.MethodPatch,
				"/api/v1/habits/"+hsHabitID+"/occurrence/steps/"+hsStepID, testUser, tc.body)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status=%d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// The toggle response carries the recomputed occurrence, so the client learns
// whether the parent flipped without a second round trip.
func TestHabitStep_SetState_ReturnsRecomputedOccurrence(t *testing.T) {
	svc := &testutil.FakeHabitService{
		SetStepStateFunc: func(c *gin.Context, habitID, stepID string, date time.Time, status string) (types.HabitOccurrenceSteps, error) {
			return types.HabitOccurrenceSteps{
				HabitID:        habitID,
				StepsTotal:     2,
				StepsCompleted: 2,
				IsCompleted:    true,
			}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch,
		"/api/v1/habits/"+hsHabitID+"/occurrence/steps/"+hsStepID, testUser,
		types.SetHabitStepStateRequest{Status: strPtr("completed")})

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got types.HabitOccurrenceSteps
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.IsCompleted || got.StepsCompleted != 2 {
		t.Errorf("got completed=%v progress=%d/%d, want true 2/2", got.IsCompleted, got.StepsCompleted, got.StepsTotal)
	}
}

func TestHabitStep_SetState_UnknownStepIs404(t *testing.T) {
	svc := &testutil.FakeHabitService{
		SetStepStateFunc: func(c *gin.Context, habitID, stepID string, date time.Time, status string) (types.HabitOccurrenceSteps, error) {
			return types.HabitOccurrenceSteps{}, types.ErrHabitStepNotFound
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch,
		"/api/v1/habits/"+hsHabitID+"/occurrence/steps/"+hsStepID, testUser,
		types.SetHabitStepStateRequest{Status: strPtr("completed")})

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestHabitStep_SetState_UnexpectedErrorIs500(t *testing.T) {
	svc := &testutil.FakeHabitService{
		SetStepStateFunc: func(c *gin.Context, habitID, stepID string, date time.Time, status string) (types.HabitOccurrenceSteps, error) {
			return types.HabitOccurrenceSteps{}, errors.New("boom")
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch,
		"/api/v1/habits/"+hsHabitID+"/occurrence/steps/"+hsStepID, testUser,
		types.SetHabitStepStateRequest{Status: strPtr("completed")})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// --- Request resolution --------------------------------------------------------

func TestSetHabitStepStateRequest_Resolve(t *testing.T) {
	if _, err := (&types.SetHabitStepStateRequest{}).Resolve(); err != types.ErrStepStateRequired {
		t.Errorf("empty request: err=%v, want ErrStepStateRequired", err)
	}
	if _, err := (&types.SetHabitStepStateRequest{Status: strPtr("nope")}).Resolve(); err != types.ErrInvalidStepStatus {
		t.Errorf("bad status: err=%v, want ErrInvalidStepStatus", err)
	}
	// Status wins over the legacy boolean when both are present.
	got, err := (&types.SetHabitStepStateRequest{Status: strPtr("skipped"), Completed: boolPtr(true)}).Resolve()
	if err != nil || got != "skipped" {
		t.Errorf("status+completed: got %q err=%v, want skipped", got, err)
	}
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
