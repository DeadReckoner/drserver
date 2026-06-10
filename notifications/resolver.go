// Package notifications holds notification-resolution and delivery logic.
//
// The Resolve function in this file is the cascade resolver: given a
// notification request, the user's settings, and any per-target override,
// it decides whether to send the notification, via which channels, with what
// delivery mode (immediate vs digest), and whether to defer for quiet hours.
//
// Resolve is intentionally PURE: no database access, no time.Now() calls,
// no logging. All inputs come from the Context and the two settings values;
// the caller is responsible for loading those from the database. This makes
// the resolver trivially testable without any infrastructure.
package notifications

import (
	"fmt"
	"time"

	"dr/server/types"
)

// Category identifies which side of the settings model a notification falls
// under. The caller (trigger or handler) always knows which category their
// notification is firing under, so we pass it explicitly rather than try to
// classify from the type string (the strings "falling_behind" and
// "milestone_at_risk" exist in both categories with different settings paths).
type Category string

const (
	CategoryMyGoals Category = "my_goals"
	CategoryFAndA   Category = "f_and_a"
)

// Context bundles the inputs needed to resolve a single notification.
// All fields are pure inputs; the resolver never mutates Context.
type Context struct {
	UserID            string    // recipient
	NotificationType  string    // e.g. "missed_task", "friend_request"
	Category          Category  // which settings path applies
	FriendID          string    // F&A only: the friend the notification is about
	AnchoredRoadmapID string    // F&A only: the anchored roadmap the notification is about
	UserTimezone      string    // IANA TZ name; empty falls back to UTC
	Now               time.Time // current time (UTC); injected for testability
}

// Decision is the resolver's output. Send=true means the caller should
// proceed to deliver via Channels in Delivery mode. Send=false with a
// non-nil SnoozeUntil means defer to that time (quiet hours). Send=false
// with a nil SnoozeUntil means skip permanently for this fire (disabled,
// muted, unknown type, etc).
type Decision struct {
	Send        bool
	Channels    []string           // subset of {"push", "email"} the user has enabled
	Delivery    types.DeliveryMode // "immediate" | "digest"
	SnoozeUntil *time.Time         // when in quiet hours, the UTC time to retry
	Reason      string             // for logging — see decisionReason constants
}

// Reason codes for Decision.Reason. Keep stable; they show up in logs.
const (
	ReasonOK              = "ok"
	ReasonGlobalMute      = "global_mute_active"
	ReasonNoChannels      = "no_channels_enabled"
	ReasonUnknownType     = "unknown_notification_type"
	ReasonUnknownCategory = "unknown_category"
	ReasonFAndADisabled   = "f_and_a_disabled"
	ReasonTypeDisabled    = "type_disabled"
	ReasonTargetMute      = "target_mute_active"
	ReasonQuietHours      = "quiet_hours_defer"

	// ReasonFrequencyCap is set by Dispatch (not Resolve) when the user has
	// already received the maximum number of immediate pushes for the rolling
	// window. Listed here so all reason codes live in one place.
	ReasonFrequencyCap = "frequency_cap"
)

// Resolve runs the cascade. It returns a Decision describing what the caller
// should do with this notification.
//
// `override` may be nil; it's only consulted for F&A notifications when
// settings.ApplyToAllFriends is false. Passing a non-nil override when
// ApplyToAllFriends is true is harmless — the override is ignored.
func Resolve(ctx Context, settings types.NotificationSettings, override *types.NotificationOverride) Decision {
	// 1. Global mute beats everything else.
	if settings.GlobalMuteUntil != nil && ctx.Now.Before(*settings.GlobalMuteUntil) {
		return Decision{Reason: ReasonGlobalMute}
	}

	// 2. Channel availability. If neither channel is on, we can't deliver
	//    anything, so bail before doing more work.
	channels := availableChannels(settings)
	if len(channels) == 0 {
		return Decision{Reason: ReasonNoChannels}
	}

	// 3-5. Resolve the effective pref based on category.
	pref, ok, reason := resolvePref(ctx, settings, override)
	if !ok {
		return Decision{Reason: reason}
	}

	// 6. Type-level enable check.
	if !pref.Enabled {
		return Decision{Reason: ReasonTypeDisabled}
	}

	// 7. Quiet hours only matter for immediate delivery. Digest deliveries
	//    fire at the user's chosen digest_delivery_hour anyway, so the
	//    digest scheduler handles their timing.
	if settings.QuietHoursEnabled && pref.Delivery == types.DeliveryImmediate {
		in, endsAt, err := InQuietHours(ctx.Now, ctx.UserTimezone, settings.QuietHoursStart, settings.QuietHoursEnd)
		if err == nil && in {
			ends := endsAt
			return Decision{
				Send:        false,
				Channels:    channels,
				Delivery:    pref.Delivery,
				SnoozeUntil: &ends,
				Reason:      ReasonQuietHours,
			}
		}
		// Bad TZ or HH:MM strings: fall through and deliver. We'd rather
		// notify the user than swallow a notification because their TZ
		// string was corrupt.
	}

	return Decision{
		Send:     true,
		Channels: channels,
		Delivery: pref.Delivery,
		Reason:   ReasonOK,
	}
}

// availableChannels returns the channels the user has globally enabled.
// Order is stable: push first, then email.
func availableChannels(s types.NotificationSettings) []string {
	channels := make([]string, 0, 2)
	if s.PushEnabled {
		channels = append(channels, types.NotificationChannelPush)
	}
	if s.EmailEnabled {
		channels = append(channels, types.NotificationChannelEmail)
	}
	return channels
}

// resolvePref looks up the effective NotificationTypePref for this notification,
// taking into account the user's defaults and any per-target override.
func resolvePref(ctx Context, settings types.NotificationSettings, override *types.NotificationOverride) (types.NotificationTypePref, bool, string) {
	switch ctx.Category {
	case CategoryMyGoals:
		p, ok := settings.MyGoalsPrefs.Lookup(ctx.NotificationType)
		if !ok {
			return p, false, ReasonUnknownType
		}
		return p, true, ""

	case CategoryFAndA:
		// Master F&A toggle gates everything in this category.
		if !settings.FAndAEnabled {
			return types.NotificationTypePref{}, false, ReasonFAndADisabled
		}
		p, ok := settings.FAndAPrefs.Lookup(ctx.NotificationType)
		if !ok {
			return p, false, ReasonUnknownType
		}

		// Per-target overrides only apply when "apply same to all" is off.
		// This is the c1 decision: per-person rows are durable; the toggle
		// just gates whether the resolver consults them.
		if !settings.ApplyToAllFriends && override != nil {
			// Per-target mute kills the notification entirely.
			if override.MuteUntil != nil && ctx.Now.Before(*override.MuteUntil) {
				return p, false, ReasonTargetMute
			}
			// Partial override: only the specific type-keys present in the
			// override map replace the default; everything else stays as the
			// user's F&A default.
			if op, ok := override.Prefs[ctx.NotificationType]; ok {
				p = op
			}
		}
		return p, true, ""

	default:
		return types.NotificationTypePref{}, false, ReasonUnknownCategory
	}
}

// InQuietHours reports whether `now` falls inside the user's quiet hours,
// and if so returns the UTC time at which quiet hours next end.
//
// Quiet hours are interpreted in the user's local timezone. They can wrap
// midnight (e.g. start=22:00, end=08:00). If start == end, quiet hours are
// treated as never active.
//
// If `tz` is empty or doesn't parse as an IANA timezone, UTC is used.
// If `start` or `end` is malformed ("HH:MM" required), returns an error;
// callers should treat that as "not in quiet hours" and deliver.
func InQuietHours(now time.Time, tz, start, end string) (in bool, endsAt time.Time, err error) {
	loc := time.UTC
	if tz != "" {
		if l, lerr := time.LoadLocation(tz); lerr == nil {
			loc = l
		}
	}
	local := now.In(loc)

	sH, sM, ok := parseHHMM(start)
	if !ok {
		return false, time.Time{}, fmt.Errorf("invalid quiet_hours_start %q", start)
	}
	eH, eM, ok := parseHHMM(end)
	if !ok {
		return false, time.Time{}, fmt.Errorf("invalid quiet_hours_end %q", end)
	}

	startMin := sH*60 + sM
	endMin := eH*60 + eM
	nowMin := local.Hour()*60 + local.Minute()

	var inQuiet bool
	wraps := startMin > endMin
	switch {
	case startMin == endMin:
		// Convention: equal = never quiet. Avoids ambiguous always-on state.
		inQuiet = false
	case !wraps:
		// Non-wrapping: e.g. 09:00–17:00
		inQuiet = nowMin >= startMin && nowMin < endMin
	default:
		// Wrapping: e.g. 22:00–08:00
		inQuiet = nowMin >= startMin || nowMin < endMin
	}
	if !inQuiet {
		return false, time.Time{}, nil
	}

	// Compute when quiet hours next end, in local time, then convert to UTC.
	end_ := time.Date(local.Year(), local.Month(), local.Day(), eH, eM, 0, 0, loc)
	if wraps && nowMin >= startMin {
		// We're past today's start; the corresponding end is tomorrow.
		end_ = end_.AddDate(0, 0, 1)
	}
	return true, end_.UTC(), nil
}

// parseHHMM parses "HH:MM" into (hour, minute, ok). Rejects malformed strings
// and out-of-range values.
func parseHHMM(s string) (h, m int, ok bool) {
	var hh, mm int
	n, err := fmt.Sscanf(s, "%d:%d", &hh, &mm)
	if n != 2 || err != nil {
		return 0, 0, false
	}
	if hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}
