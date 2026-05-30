package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NotificationSettingsHandler owns the /api/v1/notification-settings endpoints
// (the user-level settings row and the per-friend / per-anchored-goal overrides).
type NotificationSettingsHandler struct {
	db *gorm.DB
}

func NewNotificationSettingsHandler(db *gorm.DB) *NotificationSettingsHandler {
	return &NotificationSettingsHandler{db: db}
}

// =============================================================================
// Request types
// =============================================================================

// UpdateNotificationSettingsRequest is the body for PUT /notification-settings.
// All fields are optional pointers; only non-nil fields are written. This is
// what enables setting boolean fields to false (which GORM's struct-Updates
// would otherwise skip as the zero value).
type UpdateNotificationSettingsRequest struct {
	PushEnabled             *bool                                `json:"push_enabled,omitempty"`
	EmailEnabled            *bool                                `json:"email_enabled,omitempty"`
	QuietHoursEnabled       *bool                                `json:"quiet_hours_enabled,omitempty"`
	QuietHoursStart         *string                              `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd           *string                              `json:"quiet_hours_end,omitempty"`
	DigestFrequency         *types.DigestFrequency               `json:"digest_frequency,omitempty"`
	DigestDeliveryHour      *int                                 `json:"digest_delivery_hour,omitempty"`
	DigestDeliveryDayOfWeek *int                                 `json:"digest_delivery_day_of_week,omitempty"`
	GlobalMuteUntil         *time.Time                           `json:"global_mute_until,omitempty"`
	ClearGlobalMute         bool                                 `json:"clear_global_mute,omitempty"`
	MyGoalsPrefs            *types.MyGoalsPrefs                  `json:"my_goals_prefs,omitempty"`
	FAndAEnabled            *bool                                `json:"f_and_a_enabled,omitempty"`
	ApplyToAllFriends       *bool                                `json:"apply_to_all_friends,omitempty"`
	FAndAPrefs              *types.FriendsAndAccountabilityPrefs `json:"f_and_a_prefs,omitempty"`
}

// Validate returns nil if every set field is well-formed.
func (r UpdateNotificationSettingsRequest) Validate() error {
	if r.DigestFrequency != nil {
		switch *r.DigestFrequency {
		case types.DigestFrequencyNone, types.DigestFrequencyDaily, types.DigestFrequencyWeekly:
		default:
			return fmt.Errorf("digest_frequency must be none, daily, or weekly")
		}
	}
	if r.DigestDeliveryHour != nil && (*r.DigestDeliveryHour < 0 || *r.DigestDeliveryHour > 23) {
		return fmt.Errorf("digest_delivery_hour must be 0-23")
	}
	if r.DigestDeliveryDayOfWeek != nil && (*r.DigestDeliveryDayOfWeek < 0 || *r.DigestDeliveryDayOfWeek > 6) {
		return fmt.Errorf("digest_delivery_day_of_week must be 0-6")
	}
	if r.QuietHoursStart != nil {
		if _, err := time.Parse("15:04", *r.QuietHoursStart); err != nil {
			return fmt.Errorf("quiet_hours_start must be HH:MM, got %q", *r.QuietHoursStart)
		}
	}
	if r.QuietHoursEnd != nil {
		if _, err := time.Parse("15:04", *r.QuietHoursEnd); err != nil {
			return fmt.Errorf("quiet_hours_end must be HH:MM, got %q", *r.QuietHoursEnd)
		}
	}
	if r.MyGoalsPrefs != nil {
		if err := validateMyGoalsPrefs(*r.MyGoalsPrefs); err != nil {
			return err
		}
	}
	if r.FAndAPrefs != nil {
		if err := validateFAndAPrefs(*r.FAndAPrefs); err != nil {
			return err
		}
	}
	return nil
}

// toMap converts the request into a column->value map for db.Updates,
// preserving the "false is a real value" semantics that struct-Updates loses.
func (r UpdateNotificationSettingsRequest) toMap() map[string]any {
	m := map[string]any{}
	if r.PushEnabled != nil {
		m["push_enabled"] = *r.PushEnabled
	}
	if r.EmailEnabled != nil {
		m["email_enabled"] = *r.EmailEnabled
	}
	if r.QuietHoursEnabled != nil {
		m["quiet_hours_enabled"] = *r.QuietHoursEnabled
	}
	if r.QuietHoursStart != nil {
		m["quiet_hours_start"] = *r.QuietHoursStart
	}
	if r.QuietHoursEnd != nil {
		m["quiet_hours_end"] = *r.QuietHoursEnd
	}
	if r.DigestFrequency != nil {
		m["digest_frequency"] = *r.DigestFrequency
	}
	if r.DigestDeliveryHour != nil {
		m["digest_delivery_hour"] = *r.DigestDeliveryHour
	}
	if r.DigestDeliveryDayOfWeek != nil {
		m["digest_delivery_day_of_week"] = *r.DigestDeliveryDayOfWeek
	}
	// ClearGlobalMute wins over GlobalMuteUntil when both are set in the same
	// request — explicit clear is unambiguous, whereas GlobalMuteUntil = nil
	// can't be distinguished from "field omitted" in JSON.
	if r.ClearGlobalMute {
		m["global_mute_until"] = nil
	} else if r.GlobalMuteUntil != nil {
		m["global_mute_until"] = *r.GlobalMuteUntil
	}
	if r.MyGoalsPrefs != nil {
		m["my_goals_prefs"] = *r.MyGoalsPrefs
	}
	if r.FAndAEnabled != nil {
		m["f_and_a_enabled"] = *r.FAndAEnabled
	}
	if r.ApplyToAllFriends != nil {
		m["apply_to_all_friends"] = *r.ApplyToAllFriends
	}
	if r.FAndAPrefs != nil {
		m["f_and_a_prefs"] = *r.FAndAPrefs
	}
	return m
}

// UpsertNotificationOverrideRequest is the body for PUT
// /notification-settings/friends/:friendid and /anchored-goals/:roadmapid.
type UpsertNotificationOverrideRequest struct {
	Prefs          types.PrefOverrides `json:"prefs"`
	MuteUntil      *time.Time          `json:"mute_until,omitempty"`
	ClearMuteUntil bool                `json:"clear_mute_until,omitempty"`
}

func (r UpsertNotificationOverrideRequest) Validate() error {
	for typeName, pref := range r.Prefs {
		if pref.Delivery != types.DeliveryImmediate && pref.Delivery != types.DeliveryDigest {
			return fmt.Errorf("prefs[%q].delivery must be immediate or digest, got %q", typeName, pref.Delivery)
		}
	}
	return nil
}

func validateMyGoalsPrefs(p types.MyGoalsPrefs) error {
	for name, pref := range map[string]types.NotificationTypePref{
		"daily_task_reminder":       p.DailyTaskReminder,
		"task_due_reminder":         p.TaskDueReminder,
		"missed_task":               p.MissedTask,
		"falling_behind":            p.FallingBehind,
		"milestone_due":             p.MilestoneDue,
		"milestone_at_risk":         p.MilestoneAtRisk,
		"follow_on_milestone_ready": p.FollowOnMilestoneReady,
		"goal_milestone_completed":  p.GoalMilestoneCompleted,
	} {
		if err := validateDelivery(name, pref.Delivery); err != nil {
			return err
		}
	}
	return nil
}

func validateFAndAPrefs(p types.FriendsAndAccountabilityPrefs) error {
	for name, pref := range map[string]types.NotificationTypePref{
		"friend_request":          p.FriendRequest,
		"anchor_request":          p.AnchorRequest,
		"anchor_accepted_removed": p.AnchorAcceptedRemoved,
		"missed_progress":         p.MissedProgress,
		"falling_behind":          p.FallingBehind,
		"milestone_at_risk":       p.MilestoneAtRisk,
		"milestone_completed":     p.MilestoneCompleted,
		"goal_completed":          p.GoalCompleted,
		"timeline_updated":        p.TimelineUpdated,
	} {
		if err := validateDelivery(name, pref.Delivery); err != nil {
			return err
		}
	}
	return nil
}

func validateDelivery(name string, d types.DeliveryMode) error {
	if d != types.DeliveryImmediate && d != types.DeliveryDigest {
		return fmt.Errorf("%s.delivery must be immediate or digest, got %q", name, d)
	}
	return nil
}

// =============================================================================
// Global settings: GET / PUT /api/v1/notification-settings
// =============================================================================

// GetSettings returns the user's notification settings, creating the default
// row on demand if the trigger somehow missed it.
func (h *NotificationSettingsHandler) GetSettings(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	settings, err := database.EnsureNotificationSettings(userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, settings)
}

// UpdateSettings applies a partial update to the user's notification settings
// and returns the new state.
func (h *NotificationSettingsHandler) UpdateSettings(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	var req UpdateNotificationSettingsRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}
	// Ensure the row exists before patching so the first-ever PUT works for
	// users created before the trigger was deployed.
	if _, err := database.EnsureNotificationSettings(userID); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	patch := req.toMap()
	if len(patch) > 0 {
		if err := database.UpdateNotificationSettingsMap(userID, patch); err != nil {
			utils.InternalErrorResponse(c, err)
			return
		}
	}
	settings, err := database.GetNotificationSettings(userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, settings)
}

// =============================================================================
// Per-friend overrides
// =============================================================================

// ListFriendOverrides returns every per-friend override owned by the user.
// Per-anchored-goal overrides are excluded.
func (h *NotificationSettingsHandler) ListFriendOverrides(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	all, err := database.ListNotificationOverrides(userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	out := make([]types.NotificationOverride, 0, len(all))
	for _, o := range all {
		if o.FriendID != nil {
			out = append(out, o)
		}
	}
	utils.DataResponse(c, http.StatusOK, out)
}

// GetFriendOverride returns the override for a single friend, or 404 if none.
func (h *NotificationSettingsHandler) GetFriendOverride(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	friendID := c.Param("friendid")
	override, err := database.GetFriendOverride(userID, friendID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	if override == nil {
		utils.NotFoundResponse(c, "notification override")
		return
	}
	utils.DataResponse(c, http.StatusOK, override)
}

// UpsertFriendOverride creates or updates the per-friend override. The caller
// must be an accepted friend of the target user; 403 otherwise.
func (h *NotificationSettingsHandler) UpsertFriendOverride(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	friendID := c.Param("friendid")
	if friendID == userID {
		utils.BadRequestResponse(c, "cannot create an override targeting yourself")
		return
	}
	if !database.CheckAreFriends(userID, friendID) {
		utils.ForbiddenResponse(c, "you are not friends with that user")
		return
	}
	var req UpsertNotificationOverrideRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}
	o := types.NotificationOverride{
		UserID:   userID,
		FriendID: &friendID,
		Prefs:    req.Prefs,
	}
	if req.ClearMuteUntil {
		// leave MuteUntil nil
	} else if req.MuteUntil != nil {
		o.MuteUntil = req.MuteUntil
	}
	if err := database.UpsertFriendOverride(o); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	saved, err := database.GetFriendOverride(userID, friendID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, saved)
}

// DeleteFriendOverride removes the override for a friend. Idempotent: returns
// 204 whether or not a row existed.
func (h *NotificationSettingsHandler) DeleteFriendOverride(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	friendID := c.Param("friendid")
	if err := database.DeleteFriendOverride(userID, friendID); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// =============================================================================
// Per-anchored-goal overrides
// =============================================================================

// ListAnchoredGoalOverrides returns every per-anchored-goal override owned by
// the user.
func (h *NotificationSettingsHandler) ListAnchoredGoalOverrides(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	all, err := database.ListNotificationOverrides(userID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	out := make([]types.NotificationOverride, 0, len(all))
	for _, o := range all {
		if o.AnchoredRoadmapID != nil {
			out = append(out, o)
		}
	}
	utils.DataResponse(c, http.StatusOK, out)
}

// GetAnchoredGoalOverride returns the override for one anchored roadmap, or 404.
func (h *NotificationSettingsHandler) GetAnchoredGoalOverride(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	roadmapID := c.Param("roadmapid")
	override, err := database.GetAnchoredGoalOverride(userID, roadmapID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	if override == nil {
		utils.NotFoundResponse(c, "notification override")
		return
	}
	utils.DataResponse(c, http.StatusOK, override)
}

// UpsertAnchoredGoalOverride creates or updates the per-anchored-goal override.
// The caller must be the accepted anchor of the roadmap; 403 otherwise.
func (h *NotificationSettingsHandler) UpsertAnchoredGoalOverride(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	roadmapID := c.Param("roadmapid")
	isAnchor, err := database.IsAnchorOfRoadmap(userID, roadmapID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	if !isAnchor {
		utils.ForbiddenResponse(c, "you are not the anchor of that roadmap")
		return
	}
	var req UpsertNotificationOverrideRequest
	if !utils.BindJSONOrAbort(c, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		utils.BadRequestResponse(c, err.Error())
		return
	}
	o := types.NotificationOverride{
		UserID:            userID,
		AnchoredRoadmapID: &roadmapID,
		Prefs:             req.Prefs,
	}
	if !req.ClearMuteUntil && req.MuteUntil != nil {
		o.MuteUntil = req.MuteUntil
	}
	if err := database.UpsertAnchoredGoalOverride(o); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	saved, err := database.GetAnchoredGoalOverride(userID, roadmapID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	utils.DataResponse(c, http.StatusOK, saved)
}

// DeleteAnchoredGoalOverride removes the override for an anchored roadmap.
// Idempotent: returns 204 whether or not a row existed.
func (h *NotificationSettingsHandler) DeleteAnchoredGoalOverride(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		return
	}
	roadmapID := c.Param("roadmapid")
	if err := database.DeleteAnchoredGoalOverride(userID, roadmapID); err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// =============================================================================
// Helpers
// =============================================================================

// userIDFromContext extracts the authenticated user_id set by AuthMiddleware,
// returning false and writing an error response if the value is missing.
func userIDFromContext(c *gin.Context) (string, bool) {
	v, exists := c.Get("user_id")
	if !exists {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return "", false
	}
	return s, true
}

// errIsNotFound is unused here but kept as a hint for handlers that may want
// to distinguish "row absent" from a real DB error. Stays alongside the file
// to avoid scattering this idiom.
var _ = func(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
