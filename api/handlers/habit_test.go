package handlers_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"dr/server/api/handlers"
	"dr/server/internal/testutil"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func newHabitRouter(svc *testutil.FakeHabitService) *gin.Engine {
	h := &handlers.Handlers{
		Habit: handlers.NewHabitHandlerWithService(svc),
	}
	return testutil.NewTestRouterFromHandlers(h)
}

func dayOf(t time.Time) string { return t.Format("2006-01-02") }

// --- CreateHabit --------------------------------------------------------------

func TestHabit_Create_Success_DefaultsStartDateToNow(t *testing.T) {
	var received types.Habit
	svc := &testutil.FakeHabitService{
		CreateFunc: func(c *gin.Context, h types.Habit) (types.Habit, error) {
			received = h
			h.HabitID = uuid.NewString()
			return h, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits", testUser,
		types.CreateHabitRequest{Title: "meditate", RecurrenceType: types.RecurrenceDaily})

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if received.Title != "meditate" {
		t.Errorf("service got title=%q, want %q", received.Title, "meditate")
	}
	// No start_date in the request → handler defaults to now.
	if received.StartDate.IsZero() {
		t.Error("service got zero StartDate, want default of today")
	}
	if dayOf(received.StartDate) != dayOf(time.Now()) {
		t.Errorf("default StartDate=%s, want today (%s)", dayOf(received.StartDate), dayOf(time.Now()))
	}
}

func TestHabit_Create_ExplicitStartDatePassedThrough(t *testing.T) {
	var received types.Habit
	svc := &testutil.FakeHabitService{
		CreateFunc: func(c *gin.Context, h types.Habit) (types.Habit, error) {
			received = h
			return h, nil
		},
	}
	router := newHabitRouter(svc)

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits", testUser,
		types.CreateHabitRequest{Title: "run", RecurrenceType: types.RecurrenceDaily, StartDate: &start})

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if !received.StartDate.Equal(start) {
		t.Errorf("service got StartDate=%v, want %v", received.StartDate, start)
	}
}

func TestHabit_Create_ValidationFailures_Return400(t *testing.T) {
	cases := []struct {
		name string
		body types.CreateHabitRequest
	}{
		{"missing title (binding required)", types.CreateHabitRequest{RecurrenceType: types.RecurrenceDaily}},
		{"missing recurrence (binding required)", types.CreateHabitRequest{Title: "x"}},
		{"invalid recurrence type", types.CreateHabitRequest{Title: "x", RecurrenceType: "yearly"}},
		{"weekly without day mask", types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceWeekly}},
		{"monthly without day of month", types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceMonthly}},
		{"one-time without due date", types.CreateHabitRequest{Title: "x", RecurrenceType: types.RecurrenceNone}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Unstubbed service: if validation fails to reject, the
			// service panics and the test fails loudly.
			router := newHabitRouter(&testutil.FakeHabitService{})

			w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits", testUser, tc.body)

			if w.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

// --- GetHabit / GetHabits -------------------------------------------------------

func TestHabit_Get_Success(t *testing.T) {
	id := uuid.NewString()
	svc := &testutil.FakeHabitService{
		GetFunc: func(c *gin.Context, habitID string) types.Habit {
			return types.Habit{HabitID: habitID, Title: "stretch"}
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/"+id, testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	var got types.Habit
	testutil.DecodeJSON(t, w, &got)
	if got.HabitID != id {
		t.Errorf("habit_id=%q, want %q", got.HabitID, id)
	}
}

func TestHabit_Get_NotFound_Returns404(t *testing.T) {
	svc := &testutil.FakeHabitService{
		GetFunc: func(c *gin.Context, _ string) types.Habit { return types.Habit{} },
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/"+uuid.NewString(), testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHabit_List_IncludeArchivedPlumbing(t *testing.T) {
	cases := []struct {
		query string
		want  bool
	}{
		{"", false},
		{"?include_archived=true", true},
		{"?include_archived=false", false},
		{"?include_archived=1", false}, // only the literal "true" counts
	}
	for _, tc := range cases {
		t.Run("q="+tc.query, func(t *testing.T) {
			var received bool
			svc := &testutil.FakeHabitService{
				ListFunc: func(c *gin.Context, includeArchived bool) ([]types.Habit, error) {
					received = includeArchived
					return []types.Habit{}, nil
				},
			}
			router := newHabitRouter(svc)

			w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits"+tc.query, testUser, nil)

			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
			}
			if received != tc.want {
				t.Errorf("service got includeArchived=%v, want %v", received, tc.want)
			}
		})
	}
}

// --- Today / date / range -------------------------------------------------------

func TestHabit_Today_PassesUTCMidnight(t *testing.T) {
	var received time.Time
	svc := &testutil.FakeHabitService{
		ForDateFunc: func(c *gin.Context, date time.Time) ([]types.HabitWithStatus, error) {
			received = date
			return []types.HabitWithStatus{}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/today", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if received.Location() != time.UTC {
		t.Errorf("service got location=%v, want UTC", received.Location())
	}
	if received.Hour() != 0 || received.Minute() != 0 || received.Second() != 0 {
		t.Errorf("service got %v, want UTC midnight (start of day)", received)
	}
	if dayOf(received) != dayOf(time.Now().UTC()) {
		t.Errorf("service got date %s, want today %s", dayOf(received), dayOf(time.Now().UTC()))
	}
}

func TestHabit_ForDate_ValidDatePlumbed(t *testing.T) {
	var received time.Time
	svc := &testutil.FakeHabitService{
		ForDateFunc: func(c *gin.Context, date time.Time) ([]types.HabitWithStatus, error) {
			received = date
			return []types.HabitWithStatus{}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/date/2026-05-19", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	want := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	if !received.Equal(want) {
		t.Errorf("service got %v, want %v", received, want)
	}
}

func TestHabit_ForDate_InvalidDate_Returns400(t *testing.T) {
	router := newHabitRouter(&testutil.FakeHabitService{})

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/date/05-19-2026", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHabit_Range_Plumbing(t *testing.T) {
	var gotStart, gotEnd time.Time
	svc := &testutil.FakeHabitService{
		ForDateRangeFunc: func(c *gin.Context, start, end time.Time) (map[string][]types.HabitWithStatus, error) {
			gotStart, gotEnd = start, end
			return map[string][]types.HabitWithStatus{}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/range?start=2026-05-01&end=2026-05-31", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if dayOf(gotStart) != "2026-05-01" || dayOf(gotEnd) != "2026-05-31" {
		t.Errorf("service got (%s, %s), want (2026-05-01, 2026-05-31)", dayOf(gotStart), dayOf(gotEnd))
	}
}

func TestHabit_Range_BadRequests(t *testing.T) {
	cases := []struct {
		name, query string
	}{
		{"missing both", ""},
		{"missing end", "?start=2026-05-01"},
		{"missing start", "?end=2026-05-31"},
		{"bad start format", "?start=05/01/2026&end=2026-05-31"},
		{"bad end format", "?start=2026-05-01&end=tomorrow"},
		{"range over 90 days", "?start=2026-01-01&end=2026-06-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newHabitRouter(&testutil.FakeHabitService{})

			w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/range"+tc.query, testUser, nil)

			if w.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHabit_Range_Exactly90Days_Allowed(t *testing.T) {
	svc := &testutil.FakeHabitService{
		ForDateRangeFunc: func(c *gin.Context, start, end time.Time) (map[string][]types.HabitWithStatus, error) {
			return map[string][]types.HabitWithStatus{}, nil
		},
	}
	router := newHabitRouter(svc)

	// Jan 1 + 90 days = Apr 1 (2026 is not a leap year).
	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/range?start=2026-01-01&end=2026-04-01", testUser, nil)

	if w.Code != http.StatusOK {
		t.Errorf("90-day boundary rejected: got %d; body=%s", w.Code, w.Body.String())
	}
}

// --- UpdateHabit ----------------------------------------------------------------

func TestHabit_Update_PartialMergePreservesExistingFields(t *testing.T) {
	id := uuid.NewString()
	existing := types.Habit{
		HabitID:        id,
		Title:          "old title",
		Description:    "keep me",
		RecurrenceType: types.RecurrenceWeekly,
		DaysOfWeek:     int(types.Weekdays),
	}
	var sent types.Habit
	svc := &testutil.FakeHabitService{
		GetFunc: func(c *gin.Context, _ string) types.Habit { return existing },
		UpdateFunc: func(c *gin.Context, _ string, h types.Habit) (types.Habit, error) {
			sent = h
			return h, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/habits/"+id, testUser,
		map[string]any{"title": "new title"})

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if sent.Title != "new title" {
		t.Errorf("title=%q, want %q", sent.Title, "new title")
	}
	if sent.Description != "keep me" {
		t.Errorf("description=%q, want preserved %q", sent.Description, "keep me")
	}
	if sent.RecurrenceType != types.RecurrenceWeekly || sent.DaysOfWeek != int(types.Weekdays) {
		t.Errorf("recurrence fields not preserved: %+v", sent)
	}
}

func TestHabit_Update_NotFound_Returns404(t *testing.T) {
	svc := &testutil.FakeHabitService{
		GetFunc: func(c *gin.Context, _ string) types.Habit { return types.Habit{} },
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPatch, "/api/v1/habits/"+uuid.NewString(), testUser,
		map[string]any{"title": "x"})

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

// --- Delete / Archive -------------------------------------------------------------

func TestHabit_Delete_ErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{"success", nil, http.StatusOK},
		{"not found", types.ErrHabitNotFound, http.StatusNotFound},
		{"other error", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &testutil.FakeHabitService{
				DeleteFunc: func(c *gin.Context, _ string) error { return tc.serviceErr },
			}
			router := newHabitRouter(svc)

			w := testutil.Do(t, router, http.MethodDelete, "/api/v1/habits/"+uuid.NewString(), testUser, nil)

			if w.Code != tc.wantStatus {
				t.Errorf("got %d, want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

func TestHabit_Archive_ErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{"success", nil, http.StatusOK},
		{"not found", types.ErrHabitNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &testutil.FakeHabitService{
				ArchiveFunc: func(c *gin.Context, _ string) error { return tc.serviceErr },
			}
			router := newHabitRouter(svc)

			w := testutil.Do(t, router, http.MethodPost,
				"/api/v1/habits/"+uuid.NewString()+"/archive", testUser, nil)

			if w.Code != tc.wantStatus {
				t.Errorf("got %d, want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

// --- Complete / Uncomplete (idempotency) -------------------------------------------

func TestHabit_Complete_ExplicitDateAndNotesPlumbed(t *testing.T) {
	id := uuid.NewString()
	var gotDate time.Time
	var gotNotes string
	svc := &testutil.FakeHabitService{
		CompleteFunc: func(c *gin.Context, _ string, date time.Time, notes string) (*types.HabitCompletion, error) {
			gotDate, gotNotes = date, notes
			return &types.HabitCompletion{CompletionID: uuid.NewString(), HabitID: id}, nil
		},
	}
	router := newHabitRouter(svc)

	target := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	w := testutil.Do(t, router, http.MethodPost, "/api/v1/habits/"+id+"/complete", testUser,
		types.CompleteHabitRequest{Date: &target, Notes: "felt great"})

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if !gotDate.Equal(target) {
		t.Errorf("service got date=%v, want %v", gotDate, target)
	}
	if gotNotes != "felt great" {
		t.Errorf("service got notes=%q, want %q", gotNotes, "felt great")
	}
}

func TestHabit_Complete_EmptyBody_DefaultsToToday(t *testing.T) {
	var gotDate time.Time
	svc := &testutil.FakeHabitService{
		CompleteFunc: func(c *gin.Context, _ string, date time.Time, notes string) (*types.HabitCompletion, error) {
			gotDate = date
			return &types.HabitCompletion{CompletionID: uuid.NewString()}, nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/habits/"+uuid.NewString()+"/complete", testUser, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if dayOf(gotDate) != dayOf(time.Now()) {
		t.Errorf("default date=%s, want today %s", dayOf(gotDate), dayOf(time.Now()))
	}
}

func TestHabit_Complete_AlreadyCompleted_Returns400(t *testing.T) {
	// The idempotency contract: double-completing the same date is rejected
	// with a 400, not a duplicate row or a 500.
	svc := &testutil.FakeHabitService{
		CompleteFunc: func(c *gin.Context, _ string, _ time.Time, _ string) (*types.HabitCompletion, error) {
			return nil, types.ErrAlreadyCompleted
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodPost,
		"/api/v1/habits/"+uuid.NewString()+"/complete", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHabit_Uncomplete_DateParamPlumbed(t *testing.T) {
	var gotDate time.Time
	svc := &testutil.FakeHabitService{
		UncompleteFunc: func(c *gin.Context, _ string, date time.Time) error {
			gotDate = date
			return nil
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/habits/"+uuid.NewString()+"/complete?date=2026-05-19", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if dayOf(gotDate) != "2026-05-19" {
		t.Errorf("service got date=%s, want 2026-05-19", dayOf(gotDate))
	}
}

func TestHabit_Uncomplete_InvalidDate_Returns400(t *testing.T) {
	router := newHabitRouter(&testutil.FakeHabitService{})

	w := testutil.Do(t, router, http.MethodDelete,
		"/api/v1/habits/"+uuid.NewString()+"/complete?date=yesterday", testUser, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

// --- GetCompletions: limit handling -------------------------------------------------

func TestHabit_Completions_LimitHandling(t *testing.T) {
	cases := []struct {
		name, query string
		wantLimit   int
	}{
		{"default", "", 30},
		{"explicit", "?limit=7", 7},
		{"non-numeric falls back", "?limit=abc", 30},
		{"negative falls back", "?limit=-5", 30},
		{"zero falls back", "?limit=0", 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotLimit int
			svc := &testutil.FakeHabitService{
				CompletionsFunc: func(c *gin.Context, _ string, limit int) ([]types.HabitCompletion, error) {
					gotLimit = limit
					return []types.HabitCompletion{}, nil
				},
			}
			router := newHabitRouter(svc)

			w := testutil.Do(t, router, http.MethodGet,
				"/api/v1/habits/"+uuid.NewString()+"/completions"+tc.query, testUser, nil)

			if w.Code != http.StatusOK {
				t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
			}
			if gotLimit != tc.wantLimit {
				t.Errorf("service got limit=%d, want %d", gotLimit, tc.wantLimit)
			}
		})
	}
}

// --- GetHabitStats -------------------------------------------------------------------

func TestHabit_Stats_ComposesAllMetrics(t *testing.T) {
	id := uuid.NewString()
	var rateDays []int
	svc := &testutil.FakeHabitService{
		StreakFunc: func(c *gin.Context, _ string) (int, error) { return 12, nil },
		CompletionRateFunc: func(c *gin.Context, _ string, days int) (float64, error) {
			rateDays = append(rateDays, days)
			if days == 7 {
				return 0.85, nil
			}
			return 0.6, nil
		},
		TotalCompletionsFunc: func(c *gin.Context, _ string) (int64, error) { return 42, nil },
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/habits/"+id+"/stats", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	var got types.HabitStatsResponse
	testutil.DecodeJSON(t, w, &got)
	if got.CurrentStreak != 12 {
		t.Errorf("streak=%d, want 12", got.CurrentStreak)
	}
	if got.CompletionRate7d != 0.85 || got.CompletionRate30d != 0.6 {
		t.Errorf("rates=(%v, %v), want (0.85, 0.6)", got.CompletionRate7d, got.CompletionRate30d)
	}
	if got.TotalCompletions != 42 {
		t.Errorf("total=%d, want 42", got.TotalCompletions)
	}
	if len(rateDays) != 2 || rateDays[0] != 7 || rateDays[1] != 30 {
		t.Errorf("rate windows=%v, want [7 30]", rateDays)
	}
}

func TestHabit_Stats_HabitNotFound_Returns404(t *testing.T) {
	svc := &testutil.FakeHabitService{
		StreakFunc: func(c *gin.Context, _ string) (int, error) { return 0, types.ErrHabitNotFound },
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/"+uuid.NewString()+"/stats", testUser, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHabit_Stats_RateErrors_DegradeToZero(t *testing.T) {
	// Rate/total failures must not fail the whole stats call — they
	// degrade to 0 while streak still reports. Pins the graceful-degradation
	// behavior in GetHabitStats.
	svc := &testutil.FakeHabitService{
		StreakFunc: func(c *gin.Context, _ string) (int, error) { return 3, nil },
		CompletionRateFunc: func(c *gin.Context, _ string, days int) (float64, error) {
			return 0, errors.New("rate query failed")
		},
		TotalCompletionsFunc: func(c *gin.Context, _ string) (int64, error) {
			return 0, errors.New("count query failed")
		},
	}
	router := newHabitRouter(svc)

	w := testutil.Do(t, router, http.MethodGet,
		"/api/v1/habits/"+uuid.NewString()+"/stats", testUser, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (graceful degradation); body=%s", w.Code, w.Body.String())
	}
	var got types.HabitStatsResponse
	testutil.DecodeJSON(t, w, &got)
	if got.CurrentStreak != 3 {
		t.Errorf("streak=%d, want 3", got.CurrentStreak)
	}
	if got.CompletionRate7d != 0 || got.CompletionRate30d != 0 || got.TotalCompletions != 0 {
		t.Errorf("degraded metrics should be zero: %+v", got)
	}
}
