package handlers_test

import (
	"net/http"
	"testing"

	"dr/server/api/handlers"
	"dr/server/database"
	"dr/server/internal/testutil"
	"dr/server/types"
)

// =============================================================================
// Unit tests for request validation (no HTTP, no DB)
// =============================================================================

func TestUpdateNotificationSettingsRequest_Validate(t *testing.T) {
	hour := func(h int) *int { return &h }
	freq := func(f types.DigestFrequency) *types.DigestFrequency { return &f }
	str := func(s string) *string { return &s }

	cases := []struct {
		name    string
		req     handlers.UpdateNotificationSettingsRequest
		wantErr bool
	}{
		{"empty request is valid (no fields set)", handlers.UpdateNotificationSettingsRequest{}, false},
		{
			name: "valid digest frequency",
			req:  handlers.UpdateNotificationSettingsRequest{DigestFrequency: freq(types.DigestFrequencyWeekly)},
		},
		{
			name:    "invalid digest frequency",
			req:     handlers.UpdateNotificationSettingsRequest{DigestFrequency: freq("monthly")},
			wantErr: true,
		},
		{
			name:    "digest hour out of range",
			req:     handlers.UpdateNotificationSettingsRequest{DigestDeliveryHour: hour(24)},
			wantErr: true,
		},
		{
			name:    "digest hour negative",
			req:     handlers.UpdateNotificationSettingsRequest{DigestDeliveryHour: hour(-1)},
			wantErr: true,
		},
		{
			name:    "digest day of week out of range",
			req:     handlers.UpdateNotificationSettingsRequest{DigestDeliveryDayOfWeek: hour(7)},
			wantErr: true,
		},
		{
			name: "valid quiet hours",
			req: handlers.UpdateNotificationSettingsRequest{
				QuietHoursStart: str("22:00"),
				QuietHoursEnd:   str("08:00"),
			},
		},
		{
			name:    "bad quiet_hours_start format",
			req:     handlers.UpdateNotificationSettingsRequest{QuietHoursStart: str("garbage")},
			wantErr: true,
		},
		{
			name:    "bad quiet_hours_end format",
			req:     handlers.UpdateNotificationSettingsRequest{QuietHoursEnd: str("25:99")},
			wantErr: true,
		},
		{
			name: "valid my-goals prefs",
			req: handlers.UpdateNotificationSettingsRequest{
				MyGoalsPrefs: func() *types.MyGoalsPrefs { p := types.DefaultMyGoalsPrefs(); return &p }(),
			},
		},
		{
			name: "my-goals prefs with bad delivery mode",
			req: handlers.UpdateNotificationSettingsRequest{
				MyGoalsPrefs: &types.MyGoalsPrefs{
					DailyTaskReminder: types.NotificationTypePref{Enabled: true, Delivery: "instant"},
				},
			},
			wantErr: true,
		},
		{
			name: "f-and-a prefs with bad delivery mode",
			req: handlers.UpdateNotificationSettingsRequest{
				FAndAPrefs: &types.FriendsAndAccountabilityPrefs{
					FriendRequest: types.NotificationTypePref{Enabled: true, Delivery: "real-time"},
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestUpsertNotificationOverrideRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		prefs   types.PrefOverrides
		wantErr bool
	}{
		{"empty prefs map is fine", types.PrefOverrides{}, false},
		{
			name:  "valid prefs",
			prefs: types.PrefOverrides{"missed_progress": {Enabled: true, Delivery: types.DeliveryDigest}},
		},
		{
			name:    "bad delivery mode",
			prefs:   types.PrefOverrides{"missed_progress": {Enabled: true, Delivery: "blast"}},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := handlers.UpsertNotificationOverrideRequest{Prefs: tc.prefs}
			err := req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// =============================================================================
// HTTP-level tests (use testutil sqlmock + router)
//
// We intentionally focus these on validation and authorization paths, which
// either don't hit the DB or hit it once with a predictable query. Happy-path
// SQL verification belongs in an integration test against a real Postgres.
// =============================================================================

const (
	testUserID    = "11111111-1111-1111-1111-111111111111"
	testFriendID  = "22222222-2222-2222-2222-222222222222"
	testRoadmapID = "33333333-3333-3333-3333-333333333333"
)

func TestSettings_RequiresAuth(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	router := testutil.NewTestRouter(db)

	w := testutil.Do(t, router, http.MethodGet, "/api/v1/notification-settings", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestUpdateSettings_RejectsInvalidBody(t *testing.T) {
	cases := []struct {
		name string
		body any
	}{
		{
			name: "bad digest frequency",
			body: map[string]any{"digest_frequency": "monthly"},
		},
		{
			name: "bad quiet hours format",
			body: map[string]any{"quiet_hours_start": "twenty-two"},
		},
		{
			name: "digest hour out of range",
			body: map[string]any{"digest_delivery_hour": 99},
		},
		{
			name: "bad delivery mode in my_goals_prefs",
			body: map[string]any{
				"my_goals_prefs": map[string]any{
					"missed_task": map[string]any{"enabled": true, "delivery": "asap"},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := testutil.NewSqlmockDBRegex(t)
			router := testutil.NewTestRouterAs(db, testUserID)
			w := testutil.Do(t, router, http.MethodPut, "/api/v1/notification-settings", testUserID, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUpsertFriendOverride_RejectsBadUUID(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	router := testutil.NewTestRouterAs(db, testUserID)
	w := testutil.Do(t, router, http.MethodPut,
		"/api/v1/notification-settings/friends/not-a-uuid", testUserID,
		handlers.UpsertNotificationOverrideRequest{},
	)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (UUID middleware should reject), body=%s", w.Code, w.Body.String())
	}
}

func TestUpsertFriendOverride_RejectsSelfTarget(t *testing.T) {
	db, _ := testutil.NewSqlmockDBRegex(t)
	router := testutil.NewTestRouterAs(db, testUserID)
	w := testutil.Do(t, router, http.MethodPut,
		"/api/v1/notification-settings/friends/"+testUserID, testUserID,
		handlers.UpsertNotificationOverrideRequest{},
	)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestUpsertFriendOverride_ForbiddenIfNotFriends(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	// database/ functions read a package-global *gorm.DB; inject the test DB.
	t.Cleanup(database.SetDBForTest(db))

	// CheckAreFriends issues a SELECT against friendships; we return zero rows
	// to simulate "not a friend."
	mock.ExpectQuery(`SELECT .* FROM "friendships"`).
		WillReturnRows(mock.NewRows([]string{"id", "requester_id", "addressee_id", "status"}))

	router := testutil.NewTestRouterAs(db, testUserID)
	w := testutil.Do(t, router, http.MethodPut,
		"/api/v1/notification-settings/friends/"+testFriendID, testUserID,
		handlers.UpsertNotificationOverrideRequest{
			Prefs: types.PrefOverrides{"missed_progress": {Enabled: true, Delivery: types.DeliveryDigest}},
		},
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
}

func TestUpsertAnchoredGoalOverride_ForbiddenIfNotAnchor(t *testing.T) {
	db, mock := testutil.NewSqlmockDBRegex(t)
	t.Cleanup(database.SetDBForTest(db))

	// IsAnchorOfRoadmap issues a COUNT against roadmaps; return 0.
	mock.ExpectQuery(`SELECT count\(\*\) FROM "roadmaps"`).
		WillReturnRows(mock.NewRows([]string{"count"}).AddRow(0))

	router := testutil.NewTestRouterAs(db, testUserID)
	w := testutil.Do(t, router, http.MethodPut,
		"/api/v1/notification-settings/anchored-goals/"+testRoadmapID, testUserID,
		handlers.UpsertNotificationOverrideRequest{
			Prefs: types.PrefOverrides{"missed_progress": {Enabled: false, Delivery: types.DeliveryDigest}},
		},
	)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
}
