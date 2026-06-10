package handlers_test

import (
	"errors"
	"net/http"
	"testing"

	"dr/server/api/handlers"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const testUser = "11111111-1111-1111-1111-111111111111"

// newTestRouter wires a router with the given fake RoadmapService. Other
// handlers are left nil; tests must only hit roadmap routes.
func newTestRouter(svc *testutil.FakeRoadmapService) *gin.Engine {
	h := &handlers.Handlers{
		Roadmap: handlers.NewRoadmapHandlerWithService(svc),
	}
	return testutil.NewTestRouterFromHandlers(h)
}

// --- Auth wiring (smoke test that the fake middleware fronts these routes) --

func TestRoadmap_MissingAuth_Returns401(t *testing.T) {
	// No userID header → fake auth middleware rejects → handler is never
	// invoked, so the fake service's panic-on-unset behavior is also
	// confirming the middleware short-circuits.
	router := newTestRouter(&testutil.FakeRoadmapService{})
	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

// --- CRUD ------------------------------------------------------------------

func TestRoadmap_Create_Success(t *testing.T) {
	want := types.Roadmap{RoadmapID: uuid.NewString(), UserID: testUser, Goal: "learn go"}
	var receivedGoal string
	svc := &testutil.FakeRoadmapService{
		CreateFunc: func(c *gin.Context, r types.Roadmap) (types.Roadmap, error) {
			receivedGoal = r.Goal
			return want, nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps", testUser, types.Roadmap{Goal: "learn go"})

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if receivedGoal != "learn go" {
		t.Errorf("service got Goal=%q, want %q", receivedGoal, "learn go")
	}
	var got types.Roadmap
	testutil.DecodeJSON(t, w, &got)
	if got.RoadmapID != want.RoadmapID {
		t.Errorf("response roadmap_id=%q, want %q", got.RoadmapID, want.RoadmapID)
	}
}

func TestRoadmap_Create_InvalidJSON_Returns400(t *testing.T) {
	router := newTestRouter(&testutil.FakeRoadmapService{})

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps", testUser, "not-an-object")

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestRoadmap_Get_NotFound_Returns404(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		GetFullFunc: func(c *gin.Context, roadmapID string) types.Roadmap {
			return types.Roadmap{} // empty roadmap = not found
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+id, testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestRoadmap_Get_DefaultsToFullView(t *testing.T) {
	id := uuid.NewString()
	called := ""
	svc := &testutil.FakeRoadmapService{
		GetFunc:          func(c *gin.Context, _ string) types.Roadmap { called = "milestones"; return types.Roadmap{} },
		GetFullFunc:      func(c *gin.Context, _ string) types.Roadmap { called = "full"; return types.Roadmap{RoadmapID: id} },
		GetStrategicFunc: func(c *gin.Context, _ string) types.Roadmap { called = "strategic"; return types.Roadmap{} },
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+id, testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if called != "full" {
		t.Errorf("called %q service method, want %q (default view=full)", called, "full")
	}
}

func TestRoadmap_Get_ViewParamRoutesToCorrectService(t *testing.T) {
	cases := []struct {
		view, want string
	}{
		{"strategic", "strategic"},
		{"milestones", "milestones"},
		{"full", "full"},
	}
	for _, tc := range cases {
		t.Run(tc.view, func(t *testing.T) {
			id := uuid.NewString()
			called := ""
			svc := &testutil.FakeRoadmapService{
				GetFunc: func(c *gin.Context, _ string) types.Roadmap {
					called = "milestones"
					return types.Roadmap{RoadmapID: id}
				},
				GetFullFunc: func(c *gin.Context, _ string) types.Roadmap { called = "full"; return types.Roadmap{RoadmapID: id} },
				GetStrategicFunc: func(c *gin.Context, _ string) types.Roadmap {
					called = "strategic"
					return types.Roadmap{RoadmapID: id}
				},
			}
			router := newTestRouter(svc)

			w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+id+"?view="+tc.view, testUser, nil)

			if w.Code != http.StatusOK {
				t.Fatalf("status %d for view=%q; body=%s", w.Code, tc.view, w.Body.String())
			}
			if called != tc.want {
				t.Errorf("view=%q routed to %q, want %q", tc.view, called, tc.want)
			}
		})
	}
}

func TestRoadmap_Get_InvalidView_Returns400(t *testing.T) {
	id := uuid.NewString()
	router := newTestRouter(&testutil.FakeRoadmapService{})

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/"+id+"?view=nonsense", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestRoadmap_Get_BadUUID_Returns400(t *testing.T) {
	// utils.ValidateUUID middleware should reject before the handler runs.
	router := newTestRouter(&testutil.FakeRoadmapService{})

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/roadmaps/not-a-uuid", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestRoadmap_Delete_Success(t *testing.T) {
	id := uuid.NewString()
	called := false
	svc := &testutil.FakeRoadmapService{
		DeleteFunc: func(c *gin.Context, roadmapID string) error {
			called = true
			if roadmapID != id {
				t.Errorf("service got id=%q, want %q", roadmapID, id)
			}
			return nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodDelete, "/api/v1/roadmaps/"+id, testUser, nil)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Errorf("DeleteFunc not called")
	}
}

func TestRoadmap_Delete_ServiceError_Returns500(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		DeleteFunc: func(c *gin.Context, _ string) error { return errors.New("boom") },
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodDelete, "/api/v1/roadmaps/"+id, testUser, nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}

// --- Lifecycle: Restart (user scenario 1) -----------------------------------

func TestRoadmap_Restart_Success(t *testing.T) {
	id := uuid.NewString()
	want := types.Roadmap{RoadmapID: id, UserID: testUser}
	svc := &testutil.FakeRoadmapService{
		RestartFunc: func(c *gin.Context, roadmapID string) (types.Roadmap, error) {
			if roadmapID != id {
				t.Errorf("service got id=%q, want %q", roadmapID, id)
			}
			return want, nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPut, "/api/v1/roadmaps/"+id+"/restart", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var got types.Roadmap
	testutil.DecodeJSON(t, w, &got)
	if got.RoadmapID != id {
		t.Errorf("response roadmap_id=%q, want %q", got.RoadmapID, id)
	}
}

func TestRoadmap_Restart_ServiceErrors_MapToCorrectStatus(t *testing.T) {
	cases := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{"not found", errors.New("roadmap not found"), http.StatusNotFound},
		{"not started", errors.New("roadmap has not been started"), http.StatusBadRequest},
		{"unknown error", errors.New("connection refused"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.NewString()
			svc := &testutil.FakeRoadmapService{
				RestartFunc: func(c *gin.Context, _ string) (types.Roadmap, error) {
					return types.Roadmap{}, tc.serviceErr
				},
			}
			router := newTestRouter(svc)

			w := testutil.Do(t, router, http.MethodPut, "/api/v1/roadmaps/"+id+"/restart", testUser, nil)

			if w.Code != tc.wantStatus {
				t.Errorf("got %d, want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

// --- Lifecycle: ShiftDates (user scenario 2) --------------------------------

func TestRoadmap_ShiftDates_PassesDaysToService(t *testing.T) {
	cases := []int{1, 7, -3, -30}
	for _, days := range cases {
		t.Run("days="+itoa(days), func(t *testing.T) {
			id := uuid.NewString()
			var receivedDays int
			svc := &testutil.FakeRoadmapService{
				ShiftDatesFunc: func(c *gin.Context, _ string, d int) (types.Roadmap, error) {
					receivedDays = d
					return types.Roadmap{RoadmapID: id}, nil
				},
			}
			router := newTestRouter(svc)

			w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/shift", testUser,
				types.ShiftDatesRequest{Days: days})

			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
			}
			if receivedDays != days {
				t.Errorf("service got days=%d, want %d", receivedDays, days)
			}
		})
	}
}

func TestRoadmap_ShiftDates_MissingBody_Returns400(t *testing.T) {
	// ShiftDatesRequest.Days is `binding:"required"` so omitting it should
	// fail validation. Note: 0 isn't a useful shift either, and the binding
	// catches it.
	id := uuid.NewString()
	router := newTestRouter(&testutil.FakeRoadmapService{})

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/shift", testUser, map[string]any{})

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// --- Lifecycle: Pause (user scenario 3) -------------------------------------

func TestRoadmap_Pause_Success(t *testing.T) {
	id := uuid.NewString()
	called := false
	svc := &testutil.FakeRoadmapService{
		PauseFunc: func(c *gin.Context, roadmapID string) error {
			called = true
			if roadmapID != id {
				t.Errorf("service got id=%q, want %q", roadmapID, id)
			}
			return nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/pause", testUser, nil)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Error("PauseFunc not called")
	}
}

func TestRoadmap_Pause_AlreadyPaused_Returns400(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		PauseFunc: func(c *gin.Context, _ string) error {
			return errors.New("roadmap is already paused")
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/pause", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestRoadmap_Pause_NotStarted_Returns400(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		PauseFunc: func(c *gin.Context, _ string) error {
			return errors.New("roadmap has not been started")
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/pause", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestRoadmap_Pause_NotFound_Returns404(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		PauseFunc: func(c *gin.Context, _ string) error { return errors.New("roadmap not found") },
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/pause", testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

// --- Lifecycle: Resume (user scenario 4) ------------------------------------

func TestRoadmap_Resume_Success_ReturnsUpdatedRoadmap(t *testing.T) {
	id := uuid.NewString()
	want := types.Roadmap{RoadmapID: id, UserID: testUser}
	svc := &testutil.FakeRoadmapService{
		ResumeFunc: func(c *gin.Context, _ string) (types.Roadmap, error) { return want, nil },
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/resume", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	var got types.Roadmap
	testutil.DecodeJSON(t, w, &got)
	if got.RoadmapID != id {
		t.Errorf("roadmap_id=%q, want %q", got.RoadmapID, id)
	}
}

func TestRoadmap_Resume_NotPaused_Returns400(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		ResumeFunc: func(c *gin.Context, _ string) (types.Roadmap, error) {
			return types.Roadmap{}, errors.New("roadmap is not paused")
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/resume", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

// --- Lifecycle: Archive (user scenario 6) -----------------------------------

func TestRoadmap_Archive_Success(t *testing.T) {
	id := uuid.NewString()
	called := false
	svc := &testutil.FakeRoadmapService{
		ArchiveFunc: func(c *gin.Context, roadmapID string) error {
			called = true
			if roadmapID != id {
				t.Errorf("service got id=%q, want %q", roadmapID, id)
			}
			return nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/archive", testUser, nil)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Error("ArchiveFunc not called")
	}
}

func TestRoadmap_Archive_NotFound_Returns404(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		ArchiveFunc: func(c *gin.Context, _ string) error { return errors.New("roadmap not found") },
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/archive", testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

// --- Lifecycle: Restore (user scenario 7) -----------------------------------

func TestRoadmap_Restore_DefaultsStartFromTodayFalse(t *testing.T) {
	// With no body, StartFromToday should default to false (zero value).
	id := uuid.NewString()
	var receivedStartFromToday bool
	svc := &testutil.FakeRoadmapService{
		RestoreFunc: func(c *gin.Context, _ string, startFromToday bool) (types.Roadmap, error) {
			receivedStartFromToday = startFromToday
			return types.Roadmap{RoadmapID: id}, nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/restore", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if receivedStartFromToday {
		t.Error("service got start_from_today=true, want false (no body)")
	}
}

func TestRoadmap_Restore_PassesStartFromTodayThroughToService(t *testing.T) {
	id := uuid.NewString()
	var receivedStartFromToday bool
	svc := &testutil.FakeRoadmapService{
		RestoreFunc: func(c *gin.Context, _ string, startFromToday bool) (types.Roadmap, error) {
			receivedStartFromToday = startFromToday
			return types.Roadmap{RoadmapID: id}, nil
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/restore", testUser,
		types.RestoreRoadmapRequest{StartFromToday: true})

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if !receivedStartFromToday {
		t.Error("service got start_from_today=false, want true")
	}
}

func TestRoadmap_Restore_NotArchived_Returns400(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeRoadmapService{
		RestoreFunc: func(c *gin.Context, _ string, _ bool) (types.Roadmap, error) {
			return types.Roadmap{}, errors.New("roadmap is not archived")
		},
	}
	router := newTestRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/roadmaps/"+id+"/restore", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

// itoa: avoid importing strconv just to print ints in subtest names.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
