package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"dr/server/api/handlers"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func newTaskRouter(svc *testutil.FakeTaskService) *gin.Engine {
	h := &handlers.Handlers{
		Task: handlers.NewTaskHandlerWithService(svc),
	}
	return testutil.NewTestRouterFromHandlers(h)
}

// --- Task CRUD --------------------------------------------------------------

func TestTask_Create_Success(t *testing.T) {
	want := types.Task{TaskID: uuid.NewString(), UserID: testUser, Title: "write tests"}
	var receivedTitle string
	svc := &testutil.FakeTaskService{
		CreateFunc: func(c *gin.Context, task types.Task) (types.Task, error) {
			receivedTitle = task.Title
			return want, nil
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/tasks", testUser, types.Task{Title: "write tests"})

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if receivedTitle != "write tests" {
		t.Errorf("service got title=%q, want %q", receivedTitle, "write tests")
	}
}

func TestTask_Get_NotFound_Returns404(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeTaskService{
		GetFunc: func(c *gin.Context, _ string) types.Task { return types.Task{} },
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/tasks/"+id, testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestTask_Update_PassesBodyToService(t *testing.T) {
	id := uuid.NewString()
	var received types.Task
	svc := &testutil.FakeTaskService{
		UpdateFunc: func(c *gin.Context, taskID string, t types.Task) types.Task {
			received = t
			return types.Task{TaskID: taskID, Completed: t.Completed}
		},
	}
	router := newTaskRouter(svc)

	body := types.Task{Title: "updated", Completed: true}
	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/tasks/"+id, testUser, body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if received.Title != "updated" || !received.Completed {
		t.Errorf("service got %+v, want title=updated completed=true", received)
	}
}

func TestTask_Update_NotFound_Returns404(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeTaskService{
		UpdateFunc: func(c *gin.Context, _ string, _ types.Task) types.Task { return types.Task{} },
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/tasks/"+id, testUser, types.Task{Title: "x"})

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestTask_Delete_Success(t *testing.T) {
	id := uuid.NewString()
	called := false
	svc := &testutil.FakeTaskService{
		DeleteFunc: func(c *gin.Context, taskID string) error {
			called = true
			if taskID != id {
				t.Errorf("service got id=%q, want %q", taskID, id)
			}
			return nil
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodDelete, "/api/v1/tasks/"+id, testUser, nil)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	if !called {
		t.Error("DeleteFunc not called")
	}
}

func TestTask_BadUUID_Returns400(t *testing.T) {
	router := newTaskRouter(&testutil.FakeTaskService{})

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/tasks/not-a-uuid", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

// --- GetTasks: query param plumbing -----------------------------------------

func TestTask_List_PassesQueryParamsToService(t *testing.T) {
	var receivedStart, receivedEnd, receivedRoadmap string
	svc := &testutil.FakeTaskService{
		ListFunc: func(c *gin.Context, start, end, roadmapID string) []types.Task {
			receivedStart = start
			receivedEnd = end
			receivedRoadmap = roadmapID
			return []types.Task{}
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/tasks/user?start_date=2026-05-19&end_date=2026-05-26&roadmap_id=abc",
		testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if receivedStart != "2026-05-19" || receivedEnd != "2026-05-26" || receivedRoadmap != "abc" {
		t.Errorf("service got (%q, %q, %q), want (%q, %q, %q)",
			receivedStart, receivedEnd, receivedRoadmap,
			"2026-05-19", "2026-05-26", "abc")
	}
}

// --- Step status changes (user scenario 5: complete / incomplete / skipped) -

func TestTask_UpdateStep_AllThreeStatuses(t *testing.T) {
	// The user can mark a step complete, incomplete, or skipped. The
	// handler delegates to TaskService.UpdateStep with whatever Status the
	// body carried. Verify all three flow through unchanged.
	statuses := []types.StepStatus{
		types.StepStatusCompleted,
		types.StepStatusIncomplete,
		types.StepStatusSkipped,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			taskID := uuid.NewString()
			stepID := uuid.NewString()
			var received types.Step
			svc := &testutil.FakeTaskService{
				UpdateStepFunc: func(c *gin.Context, tID, sID string, s types.Step) types.Step {
					received = s
					if tID != taskID {
						t.Errorf("taskID: got %q, want %q", tID, taskID)
					}
					if sID != stepID {
						t.Errorf("stepID: got %q, want %q", sID, stepID)
					}
					// Echo back with the same status and a populated ID so the
					// handler doesn't 404.
					return types.Step{StepID: stepID, Status: s.Status, Completed: s.Completed}
				},
			}
			router := newTaskRouter(svc)

			body := types.Step{Status: string(status), Completed: status == types.StepStatusCompleted}
			w := testutil.Do(t, router, http.MethodPatch,
				"/api/v1/tasks/"+taskID+"/steps/"+stepID,
				testUser, body)

			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
			}
			if received.Status != string(status) {
				t.Errorf("service got status=%q, want %q", received.Status, status)
			}

			var got types.Step
			testutil.DecodeJSON(t, w, &got)
			if got.Status != string(status) {
				t.Errorf("response status=%q, want %q", got.Status, status)
			}
		})
	}
}

func TestTask_UpdateStep_NotFound_Returns404(t *testing.T) {
	taskID := uuid.NewString()
	stepID := uuid.NewString()
	svc := &testutil.FakeTaskService{
		UpdateStepFunc: func(c *gin.Context, _, _ string, _ types.Step) types.Step {
			return types.Step{} // empty step = not found
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch,
		"/api/v1/tasks/"+taskID+"/steps/"+stepID,
		testUser, types.Step{Status: string(types.StepStatusCompleted)})

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

// --- CompleteAllSteps: returns steps_completed count ------------------------

func TestTask_CompleteAllSteps_ReturnsCount(t *testing.T) {
	taskID := uuid.NewString()
	svc := &testutil.FakeTaskService{
		CompleteAllStepsFunc: func(c *gin.Context, id string) (int64, error) {
			if id != taskID {
				t.Errorf("service got id=%q, want %q", id, taskID)
			}
			return 5, nil
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/tasks/"+taskID+"/steps/complete-all", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	// JSON numbers decode to float64 in interface{}
	if n, ok := got["steps_completed"].(float64); !ok || int(n) != 5 {
		t.Errorf("steps_completed: got %v, want 5", got["steps_completed"])
	}
	if id, _ := got["task_id"].(string); id != taskID {
		t.Errorf("task_id: got %q, want %q", id, taskID)
	}
}

func TestTask_CompleteAllSteps_TaskNotFound_Returns404(t *testing.T) {
	taskID := uuid.NewString()
	svc := &testutil.FakeTaskService{
		CompleteAllStepsFunc: func(c *gin.Context, _ string) (int64, error) {
			return 0, errors.New("task not found")
		},
	}
	router := newTaskRouter(svc)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/tasks/"+taskID+"/steps/complete-all", testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}
