package notifications

import (
	"context"
	"time"

	apiutils "dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
)

// DispatchOptions describes a single notification the caller wants to deliver.
// Title/Body/Data are pass-through to the push payload; Category/NotificationType/
// FriendID/AnchoredRoadmapID feed the cascade resolver; DedupKey backs the
// idempotency log.
type DispatchOptions struct {
	UserID            string         // recipient
	Category          Category       // CategoryMyGoals | CategoryFAndA
	NotificationType  string         // e.g. "friend_request"; must be known to the resolver
	DedupKey          string         // unique per-event key; used as notification_delivery_log.dedup_key
	FriendID          string         // F&A only: the friend the notification is about
	AnchoredRoadmapID string         // F&A only: the anchored roadmap the notification is about
	Title             string         // push title
	Body              string         // push body
	Data              map[string]any // push payload
}

// PushSender is the seam between the resolver/idempotency layer and the actual
// push delivery. Production uses DefaultPushSender (which wraps
// utils.SendPushNotification); tests inject a fake to assert what got sent.
type PushSender func(userID, notificationType, title, body string, data map[string]any)

// DefaultPushSender is the production push path.
var DefaultPushSender PushSender = apiutils.SendPushNotification

// Dispatcher bundles the side-effecting dependencies so they can be substituted
// in tests. Production callers use Dispatch() which uses the package-default
// instance wired to the real database + push provider.
type Dispatcher struct {
	LoadSettings         func(userID string) (types.NotificationSettings, error)
	LoadUserTimezone     func(userID string) string
	LoadFriendOverride   func(userID, friendID string) (*types.NotificationOverride, error)
	LoadAnchoredOverride func(userID, roadmapID string) (*types.NotificationOverride, error)
	RecordDelivery       func(userID, notificationType, dedupKey, channel string) (bool, error)

	// QueuePending stores a notification for later delivery (UPSERT on the
	// pending_notifications table). When non-nil and the resolver says
	// SnoozeUntil, Dispatch calls this instead of dropping the notification.
	// In production this is wired to database.UpsertPendingNotification.
	// Leave nil in tests that don't care about deferral; Dispatch will fall
	// back to the v1 drop behavior.
	QueuePending func(p types.PendingNotification) error

	// QueueDigest buffers a digest-mode notification for the user's next
	// daily/weekly digest email (UPSERT into digest_items). Called when the
	// resolver says Send=true with Delivery=DeliveryDigest. Leave nil in
	// tests that don't care about digesting; Dispatch will fall back to the
	// v1 stopgap of sending digest-mode pushes immediately.
	QueueDigest func(item types.DigestItem) error

	// CountRecentPushes returns how many push deliveries the user has claimed
	// in the delivery log since the given time, across all notification
	// types. Backs the per-user frequency cap; leave nil to disable the cap
	// (tests that don't care about it).
	CountRecentPushes func(userID string, since time.Time) (int64, error)

	Send PushSender
	Now  func() time.Time
}

// Frequency cap: at most maxPushesPerWindow immediate pushes per user per
// rolling pushCapWindow, across all notification types. A safety valve, not
// a tuning instrument — per-type dedup keys (once-ever, once-per-ISO-week)
// do the day-to-day rate limiting; this stops pathological bursts (e.g. many
// roadmaps tripping at once) from carpet-bombing one phone.
//
// Tuning knobs for Step 12's "watch prod and adjust" follow-up.
const (
	maxPushesPerWindow = 10
	pushCapWindow      = 24 * time.Hour
)

// production is the singleton Dispatcher wired to the real database + push.
var production = Dispatcher{
	LoadSettings: database.EnsureNotificationSettings,
	LoadUserTimezone: func(userID string) string {
		u, err := database.GetUserByID(userID)
		if err != nil {
			return "" // resolver falls back to UTC
		}
		return u.Timezone
	},
	LoadFriendOverride:   database.GetFriendOverride,
	LoadAnchoredOverride: database.GetAnchoredGoalOverride,
	RecordDelivery:       database.RecordDelivery,
	QueuePending:         database.UpsertPendingNotification,
	QueueDigest:          database.UpsertDigestItem,
	CountRecentPushes:    database.CountRecentPushes,
	Send:                 DefaultPushSender,
	Now:                  time.Now,
}

// Dispatch is the production entry point. Loads settings + (optionally)
// override, runs the cascade resolver, idempotently records delivery, and
// sends if the resolver said to. Fire-and-forget; failures are logged.
// Returns the Decision so callers (and tests) can audit what happened.
func Dispatch(opts DispatchOptions) Decision {
	return production.Dispatch(opts)
}

// Dispatch on the Dispatcher receiver is the testable form.
func (d Dispatcher) Dispatch(opts DispatchOptions) Decision {
	ctx := log.WithLogger(context.Background(), log.NewLogger(opts.UserID))

	settings, err := d.LoadSettings(opts.UserID)
	if err != nil {
		log.Error(ctx, "notifications.Dispatch: failed to load settings", err)
		return Decision{Reason: "settings_load_error"}
	}

	tz := d.LoadUserTimezone(opts.UserID)

	// Per-target override is only consulted when ApplyToAllFriends is off.
	// The resolver enforces this gating internally too, but skipping the DB
	// hit when we know it won't be used keeps Dispatch fast in the common case.
	var override *types.NotificationOverride
	if opts.Category == CategoryFAndA && !settings.ApplyToAllFriends {
		switch {
		case opts.AnchoredRoadmapID != "":
			override, err = d.LoadAnchoredOverride(opts.UserID, opts.AnchoredRoadmapID)
		case opts.FriendID != "":
			override, err = d.LoadFriendOverride(opts.UserID, opts.FriendID)
		}
		if err != nil {
			// Override load failures shouldn't block the notification —
			// the resolver will fall back to FAndAPrefs default.
			log.Info(ctx, "notifications.Dispatch: failed to load override (continuing with default)", err)
			override = nil
		}
	}

	decision := Resolve(Context{
		UserID:            opts.UserID,
		NotificationType:  opts.NotificationType,
		Category:          opts.Category,
		FriendID:          opts.FriendID,
		AnchoredRoadmapID: opts.AnchoredRoadmapID,
		UserTimezone:      tz,
		Now:               d.Now(),
	}, settings, override)

	if !decision.Send {
		// Step 9: SnoozeUntil now means "defer until this time" — UPSERT a
		// pending_notifications row that the DeliverPendingNotifications
		// cron will pick up. Permanent skips (disabled, muted, unknown
		// type) still drop immediately.
		if decision.SnoozeUntil != nil && d.QueuePending != nil {
			if err := d.QueuePending(types.PendingNotification{
				UserID:            opts.UserID,
				NotificationType:  opts.NotificationType,
				DedupKey:          opts.DedupKey,
				Channel:           types.NotificationChannelPush,
				Category:          string(opts.Category),
				FriendID:          stringPtrOrNil(opts.FriendID),
				AnchoredRoadmapID: stringPtrOrNil(opts.AnchoredRoadmapID),
				Title:             opts.Title,
				Body:              opts.Body,
				Data:              opts.Data,
				ScheduledFor:      *decision.SnoozeUntil,
			}); err != nil {
				log.Error(ctx, "notifications.Dispatch: queue pending failed", err)
			}
		}
		log.Debug(ctx, "notifications.Dispatch: not sending",
			"type", opts.NotificationType,
			"reason", decision.Reason,
			"snooze_until", decision.SnoozeUntil,
		)
		return decision
	}

	// Frequency cap: bound immediate pushes per user per rolling window.
	// Checked BEFORE the delivery-log claim so a capped event isn't burned —
	// scan-driven triggers re-candidate on their next tick and deliver once
	// the window has room. Digest-mode deliveries skip the cap; they collapse
	// into a single email anyway.
	if decision.Delivery != types.DeliveryDigest && d.CountRecentPushes != nil {
		n, err := d.CountRecentPushes(opts.UserID, d.Now().Add(-pushCapWindow))
		if err != nil {
			// Fail open — a counting error shouldn't block notifications.
			log.Error(ctx, "notifications.Dispatch: frequency cap count failed (continuing)", err)
		} else if n >= maxPushesPerWindow {
			decision.Send = false
			decision.Reason = ReasonFrequencyCap
			log.Info(ctx, "notifications.Dispatch: frequency cap hit, skipping",
				"type", opts.NotificationType,
				"recent_pushes", n,
			)
			return decision
		}
	}

	// Idempotency: claim the delivery log row first. If a duplicate trigger
	// already claimed it, skip silently.
	inserted, err := d.RecordDelivery(opts.UserID, opts.NotificationType, opts.DedupKey, types.NotificationChannelPush)
	if err != nil {
		// Don't fail the whole event — log and skip. The next firing of the
		// same trigger will retry naturally.
		log.Error(ctx, "notifications.Dispatch: delivery log write failed", err)
		return decision
	}
	if !inserted {
		log.Debug(ctx, "notifications.Dispatch: already delivered, skipping",
			"type", opts.NotificationType,
			"dedup_key", opts.DedupKey,
		)
		return decision
	}

	// Digest-mode delivery: buffer for the next daily/weekly digest email
	// instead of pushing immediately. The DeliverDueDigests cron drains
	// these per-user when their digest hour rolls around. If QueueDigest
	// isn't wired (older tests, partial deploys), fall back to immediate
	// push to avoid silently dropping the notification.
	if decision.Delivery == types.DeliveryDigest && d.QueueDigest != nil {
		if err := d.QueueDigest(types.DigestItem{
			UserID:           opts.UserID,
			NotificationType: opts.NotificationType,
			DedupKey:         opts.DedupKey,
			Title:            opts.Title,
			Body:             opts.Body,
			Data:             opts.Data,
		}); err != nil {
			log.Error(ctx, "notifications.Dispatch: queue digest failed", err)
		}
		return decision
	}

	d.Send(opts.UserID, opts.NotificationType, opts.Title, opts.Body, opts.Data)
	return decision
}

// stringPtrOrNil returns &s if s != "" else nil. Used to keep nullable UUID
// columns in pending_notifications NULL rather than empty strings.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
