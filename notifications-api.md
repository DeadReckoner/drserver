# Notification Settings API

Frontend integration guide for the notification settings system. Covers user
timezone, the global/per-friend/per-anchored-goal settings endpoints,
behavioral notes, and what's not yet live.

> **Audience:** the drmobile team plugging the iOS/Android app into the new
> notification preferences UI (Account → Notifications & Settings, plus
> Account → Friends & Accountability).

## Status overview (what's live)

| Feature | Status | Where |
|---|---|---|
| User timezone column on `users` | **Live** | `PATCH /api/v1/users/:id` |
| Global notification settings (channels, quiet hours, digest prefs, mute) | **Live** | `GET/PUT /api/v1/notification-settings` |
| Per-notification-type prefs (my-goals + F&A) | **Live** | Same endpoint |
| "Apply same to all friends" toggle | **Live** | Same endpoint (`apply_to_all_friends`) |
| Per-friend overrides | **Live** | `GET/PUT/DELETE /api/v1/notification-settings/friends/:friendid` |
| Per-anchored-goal overrides | **Live** | `GET/PUT/DELETE /api/v1/notification-settings/anchored-goals/:roadmapid` |
| Friend-request push goes through settings | **Live** | Recipient must have `f_and_a_enabled` |
| Anchor-request push goes through settings | **Live** | Same |
| Anchor-accepted push goes through settings | **Live** | Same |
| Cron scheduler infrastructure | **Stub only** | Logging only, no triggers fire yet |
| Falling-behind / missed-task / at-risk detection | **Not yet** | Steps 7–8 |
| Quiet-hours deferred delivery | **Drops in v1** | Step 9 will defer; until then a push during quiet hours is skipped |
| Daily / weekly digest delivery | **Not yet** | Step 10 |
| Email channel | **Not yet** | Step 11 (toggle is wired but no provider) |

---

## Quick start (app launch flow)

1. **On signup or login**, send the device's IANA timezone:
   ```ts
   await api.patch(`/api/v1/users/${userId}`, {
     timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, // e.g. "America/Los_Angeles"
   });
   ```
   This populates the column the cron uses for quiet-hours math and 9am-local
   batching. Existing users get `"UTC"` until they next launch the app.

2. **On Notifications & Settings screen mount**, fetch the full settings:
   ```ts
   const { data: settings } = await api.get('/api/v1/notification-settings');
   ```
   The trigger seeds a default row when a user is created, so every authenticated
   user has settings to fetch — no 404 path.

3. **When a user changes a toggle**, send a partial update:
   ```ts
   await api.put('/api/v1/notification-settings', { push_enabled: false });
   ```
   Every field is optional. Only sent fields are written. See [PUT semantics](#put-semantics) below.

4. **On the Friends & Accountability screen**, list overrides:
   ```ts
   const friends = await api.get('/api/v1/notification-settings/friends');
   const goals   = await api.get('/api/v1/notification-settings/anchored-goals');
   ```
   These return only the friends/goals the user has explicit overrides for —
   typically empty for new users.

---

## Data model

### TypeScript types (mirror these in drmobile)

```ts
// Single notification type's config — enabled flag + delivery mode.
type NotificationTypePref = {
  enabled: boolean;
  delivery: "immediate" | "digest";
};

// Per-type prefs for the user's OWN goals. JSON keys are stable.
type MyGoalsPrefs = {
  daily_task_reminder:       NotificationTypePref;
  task_due_reminder:         NotificationTypePref;
  missed_task:               NotificationTypePref;
  falling_behind:            NotificationTypePref;
  milestone_due:             NotificationTypePref;
  milestone_at_risk:         NotificationTypePref;
  follow_on_milestone_ready: NotificationTypePref;
  goal_milestone_completed:  NotificationTypePref;
  agent_updates:             NotificationTypePref; // AI generation finished (success + failure)
};

// Per-type prefs for Friends & Accountability notifications.
type FriendsAndAccountabilityPrefs = {
  friend_request:          NotificationTypePref;
  anchor_request:          NotificationTypePref;
  anchor_accepted_removed: NotificationTypePref;
  missed_progress:         NotificationTypePref;
  falling_behind:          NotificationTypePref;
  milestone_at_risk:       NotificationTypePref;
  milestone_completed:     NotificationTypePref;
  goal_completed:          NotificationTypePref;
  timeline_updated:        NotificationTypePref;
  nudges:                  NotificationTypePref; // partner nudges (roadmap + habit)
  messages:                NotificationTypePref; // new chat message
};

type DigestFrequency = "none" | "daily" | "weekly";

// The full settings row returned by GET /api/v1/notification-settings.
type NotificationSettings = {
  user_id: string;                  // UUID

  // Global channel toggles
  push_enabled:  boolean;
  email_enabled: boolean;

  // Quiet hours (interpreted in the user's timezone)
  quiet_hours_enabled: boolean;
  quiet_hours_start:   string;      // "HH:MM"
  quiet_hours_end:     string;      // "HH:MM"

  // Digest
  digest_frequency:            DigestFrequency;
  digest_delivery_hour:        number;        // 0-23
  digest_delivery_day_of_week: number;        // 0-6 (0=Sunday)

  // Global mute — when set in the future, every notification skips
  global_mute_until?: string;       // ISO 8601, or omitted

  // Per-type prefs for the user's own goals
  my_goals_prefs: MyGoalsPrefs;

  // Friends & Accountability
  f_and_a_enabled:       boolean;
  apply_to_all_friends:  boolean;
  f_and_a_prefs:         FriendsAndAccountabilityPrefs;

  created_at: string;
  updated_at: string;
};

// Per-friend or per-anchored-goal override. Exactly one of friend_id /
// anchored_roadmap_id is set; never both.
type NotificationOverride = {
  id: string;
  user_id: string;
  friend_id?: string;               // present iff this is a per-friend override
  anchored_roadmap_id?: string;     // present iff this is a per-anchored-goal override

  // Partial prefs — only the type-keys present here override the user's
  // f_and_a_prefs default. All other types fall through to the default.
  // Shape: { [notificationType]: NotificationTypePref }
  prefs: { [notificationType: string]: NotificationTypePref };

  mute_until?: string;              // per-target mute (ISO 8601)
  created_at: string;
  updated_at: string;
};
```

### Defaults

The trigger inserts a default settings row when a user is created. **Every
default is conservative — notifications are on, but quiet hours are off and
nothing is muted.** This matches the existing UX (everyone gets pushes
unconditionally before this work) — users only see fewer notifications if
they opt out via the UI.

```ts
const defaults: NotificationSettings = {
  user_id: "...",

  push_enabled:        true,
  email_enabled:       true,

  quiet_hours_enabled: false,
  quiet_hours_start:   "22:00",
  quiet_hours_end:     "08:00",

  digest_frequency:            "none",  // no digests until user opts in
  digest_delivery_hour:        8,
  digest_delivery_day_of_week: 1,       // Monday

  global_mute_until: undefined,

  my_goals_prefs: {
    daily_task_reminder:       { enabled: true, delivery: "immediate" },
    task_due_reminder:         { enabled: true, delivery: "immediate" },
    missed_task:               { enabled: true, delivery: "digest"    },
    falling_behind:            { enabled: true, delivery: "digest"    },
    milestone_due:             { enabled: true, delivery: "immediate" },
    milestone_at_risk:         { enabled: true, delivery: "immediate" },
    follow_on_milestone_ready: { enabled: true, delivery: "immediate" },
    goal_milestone_completed:  { enabled: true, delivery: "immediate" },
    agent_updates:             { enabled: true, delivery: "immediate" },
  },

  f_and_a_enabled:      true,
  apply_to_all_friends: true,
  f_and_a_prefs: {
    friend_request:          { enabled: true, delivery: "immediate" },
    anchor_request:          { enabled: true, delivery: "immediate" },
    anchor_accepted_removed: { enabled: true, delivery: "immediate" },
    missed_progress:         { enabled: true, delivery: "digest"    },
    falling_behind:          { enabled: true, delivery: "digest"    },
    milestone_at_risk:       { enabled: true, delivery: "immediate" },
    milestone_completed:     { enabled: true, delivery: "digest"    },
    goal_completed:          { enabled: true, delivery: "immediate" },
    timeline_updated:        { enabled: true, delivery: "digest"    },
    nudges:                  { enabled: true, delivery: "immediate" },
    messages:                { enabled: true, delivery: "immediate" },
  },

  created_at: "...",
  updated_at: "...",
};
```

The choice of `digest` vs `immediate` per type follows the spec: routine
events (missed task, falling behind, missed progress, timeline updated) are
batched by default; status changes (milestone at-risk, milestone due, friend
request, anchor request, goal completed) are immediate.

---

## Endpoints

All notification-settings endpoints require Bearer-token auth (same as the
rest of the API). The authenticated user is always the row owner — there is
no path for one user to read or modify another user's settings.

### User timezone

#### PATCH /api/v1/users/:id
Update the user's timezone (along with any other user fields). This is the
*only* user endpoint affected by the notification work — the rest is
unchanged.

**Request body:**
```json
{
  "timezone": "America/Los_Angeles"
}
```

`timezone` must be a valid IANA name. The server doesn't validate it
explicitly; the cron's quiet-hours math will fall back to UTC if it can't be
parsed, so a typo silently degrades to UTC behavior. **Best practice:** send
`Intl.DateTimeFormat().resolvedOptions().timeZone` on every cold start so
the value stays in sync if the user changes device timezone.

**Response (200 OK):** the updated User row, same shape as
`GET /api/v1/users` adds a `timezone` field.

---

### Global settings

#### GET /api/v1/notification-settings
Fetch the user's full settings row.

**Example:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/notification-settings"
```

**Response (200 OK):** a `NotificationSettings` object (see [Data model](#data-model)).

If somehow the row doesn't exist (shouldn't happen — trigger seeds it), the
server creates a default row on demand and returns it. No 404 path.

---

#### PUT /api/v1/notification-settings
Apply a partial update. Returns the new state.

**Request body — every field is optional:**
```json
{
  "push_enabled": false,
  "quiet_hours_enabled": true,
  "quiet_hours_start": "21:30",
  "quiet_hours_end": "07:00",
  "digest_frequency": "daily",
  "digest_delivery_hour": 9,
  "f_and_a_enabled": true,
  "apply_to_all_friends": false,
  "my_goals_prefs": {
    "missed_task": { "enabled": false, "delivery": "digest" }
    // ... other fields if you want to override them
  }
}
```

**Response (200 OK):** the updated full `NotificationSettings` object.

##### PUT semantics

- **Only sent fields are written.** Omit a field to leave it alone.
- **Booleans:** because `false` is a meaningful value the API uses
  pointer-style decoding internally — sending `"push_enabled": false`
  actually persists `false` (not "skipped as zero value").
- **`global_mute_until`:** to clear an active mute, send
  `{ "clear_global_mute": true }`. To set or extend a mute, send the
  ISO 8601 timestamp directly. Sending both in the same request: clear wins.
- **`my_goals_prefs` / `f_and_a_prefs`:** if you send these, you must send
  the **full struct** (every field). Partial maps are not supported on the
  global object — that's what overrides are for.

##### Validation rules (return 400 on violation)

| Field | Rule |
|---|---|
| `digest_frequency` | must be one of `"none"`, `"daily"`, `"weekly"` |
| `digest_delivery_hour` | integer in `[0, 23]` |
| `digest_delivery_day_of_week` | integer in `[0, 6]` (0 = Sunday) |
| `quiet_hours_start` / `quiet_hours_end` | string matching `HH:MM` (24-hour) |
| any `NotificationTypePref.delivery` | must be `"immediate"` or `"digest"` |

Error response shape:
```json
{
  "error": "Bad Request",
  "message": "digest_frequency must be none, daily, or weekly",
  "code": 400
}
```

---

### Per-friend overrides

These let the user customize F&A notifications for a specific friend
(e.g. "mute Paul," "send Mike's missed-progress alerts immediately").

**Important:** overrides only take effect when the user has set
`apply_to_all_friends: false` on their global settings. When the toggle is
`true`, override rows persist but are ignored by the resolver. Flipping the
toggle back to `false` restores them as-is. See [c1 toggle behavior](#apply-to-all-toggle-c1).

#### GET /api/v1/notification-settings/friends
List every per-friend override the user has created.

**Response (200 OK):**
```json
{
  "data": [
    {
      "id": "override-uuid",
      "user_id": "your-user-uuid",
      "friend_id": "friend-user-uuid",
      "prefs": {
        "missed_progress": { "enabled": true, "delivery": "immediate" }
      },
      "mute_until": null,
      "created_at": "2026-05-18T12:00:00Z",
      "updated_at": "2026-05-18T12:00:00Z"
    }
  ]
}
```

Empty array if the user has no per-friend overrides — common for new users.

#### GET /api/v1/notification-settings/friends/:friendid
Fetch a single override.

**Response (200 OK):** the `NotificationOverride` object.

**Response (404 Not Found):** no override exists for this friend.
```json
{ "error": "Not Found", "message": "notification override not found", "code": 404 }
```

#### PUT /api/v1/notification-settings/friends/:friendid
Create or update the override (upsert).

**Authorization:** the caller must have an accepted friendship with `:friendid`.
Returns 403 if not friends, 400 if `:friendid` equals the caller's own user ID.

**Request body:**
```json
{
  "prefs": {
    "missed_progress": { "enabled": false, "delivery": "digest" },
    "milestone_at_risk": { "enabled": true, "delivery": "immediate" }
  },
  "mute_until": "2026-05-20T00:00:00Z"
}
```

- `prefs`: a **partial** map. Only the keys you specify override the user's
  F&A default for this friend. All other notification types for this friend
  fall through to the user's `f_and_a_prefs`.
- `mute_until`: optional. Sets a per-friend mute window (e.g. for the spec's
  "mute for 24 hours / 7 days / until turned back on" UI).
- To clear a mute without removing the override entirely, send
  `{ "clear_mute_until": true }`.

**Response (200 OK):** the saved override row.

**Errors:**
- `400 Bad Request` — bad UUID, self-targeted override, malformed delivery mode
- `403 Forbidden` — not friends with the target user

#### DELETE /api/v1/notification-settings/friends/:friendid
Remove the override. Idempotent — returns 204 whether or not a row existed.

**Response (204 No Content)** — empty body.

---

### Per-anchored-goal overrides

These let an anchor customize the F&A notifications they receive about a
*specific roadmap they're anchoring*. Per-anchored-goal overrides scope to
roadmaps the user has an accepted `AnchorRequest` for — they cannot be
created for arbitrary roadmaps.

The endpoint shapes mirror per-friend overrides exactly; the only
differences are the URL path and the authorization check.

#### GET /api/v1/notification-settings/anchored-goals
List every per-anchored-goal override.

#### GET /api/v1/notification-settings/anchored-goals/:roadmapid
Fetch a single override. 404 if none.

#### PUT /api/v1/notification-settings/anchored-goals/:roadmapid
Upsert. The `roadmapid` must be a roadmap where the caller is the accepted
anchor (`roadmaps.anchor_id = caller_id`). Returns 403 otherwise.

Same request body as the friend variant.

#### DELETE /api/v1/notification-settings/anchored-goals/:roadmapid
Remove. Idempotent, returns 204.

---

## Behavioral notes

### Apply-to-all toggle (c1)

This is the most subtle bit of the model, worth designing the UI around.

`notification_settings.apply_to_all_friends` is a single boolean that gates
whether per-friend / per-anchored-goal overrides take effect. **The
overrides themselves persist regardless of the toggle's value.**

Concrete scenarios:

| State | Behavior |
|---|---|
| `apply_to_all_friends: true` (default) | Every F&A notification uses `f_and_a_prefs`. Overrides exist in the DB but are ignored. Per-target mute is also ignored. |
| `apply_to_all_friends: false` | When sending an F&A notification, the resolver looks up the override for the target friend / anchored goal. If one exists, the mute and any per-type prefs in `prefs` take effect; otherwise it falls back to `f_and_a_prefs`. |
| User flips from `true` to `false` | Previously created override rows immediately become active. |
| User flips from `false` to `true` | Override rows still exist but are no longer consulted. |
| User flips back to `false` again | Override rows are still there and reactivate exactly as before. |

**UX implication:** when the user toggles "Apply same to all friends" OFF,
show their existing per-person customizations (they exist). When they
toggle ON, you can hide the per-person section, but don't delete or warn
about loss — nothing is lost.

### What gets gated today

**Every** server-originated push now routes through the settings system. For
each, the recipient's settings apply:
- If the type's pref is disabled (or `f_and_a_enabled = false` for F&A types),
  no push fires.
- If `apply_to_all_friends = false` AND an override disables/mutes the target,
  no push fires.
- If `quiet_hours_enabled = true` AND the current time is in the window, the
  push is **deferred** to the end of quiet hours (except `message_new` — see
  below).
- Duplicate triggers within the same delivery key don't double-fire — the
  server tracks `(user, type, dedup_key)` for idempotency.
- A per-user frequency cap (max 10 immediate pushes / rolling 24h) backstops
  pathological bursts. `message_new` is exempt.

Which pref toggle controls each notification type:

| Notification type(s) | Category | Controlled by toggle |
|---|---|---|
| `friend_request` | F&A | `friend_request` |
| `friend_accepted`, `friend_rejected` | F&A | `friend_request` |
| `anchor_request`, `habit_anchor_request` | F&A | `anchor_request` |
| `anchor_accepted_removed`, `anchor_rejected` | F&A | `anchor_accepted_removed` |
| `roadmap_nudge`, `habit_nudge` | F&A | `nudges` |
| `message_new` | F&A | `messages` |
| `missed_progress`, `falling_behind`, `milestone_at_risk`, `milestone_completed`, `goal_completed`, `timeline_updated` | F&A | same-named key |
| `roadmap_complete`, `roadmap_failed`, `next_milestone_complete`, `next_milestone_failed` | My Goals | `agent_updates` |
| `missed_task`, `falling_behind`, `milestone_due`, `milestone_at_risk`, daily/due reminders, etc. | My Goals | same-named key |

Note several types **share** one toggle (e.g. accept/decline ride the original
request's toggle). There is intentionally no separate switch for them in the
settings UI — surface one control per toggle key, not one per type string.

#### `messages` is special

New-message pushes are conversational, so Dispatch treats `message_new`
differently from every other type:
- **Cap-exempt:** an active chat won't burn the 10/24h frequency cap (and
  therefore can't starve other notification types).
- **Quiet-hours drop, not defer:** messages that arrive during quiet hours are
  dropped rather than queued — a stack of stale chat pushes at 8am is noise,
  and the messages are already in the thread.

Muting a specific friend (`apply_to_all_friends = false` + per-friend mute)
still mutes their messages, since the sender is the override target.

### Push tokens and the existing badge endpoint

The settings system **does not** replace the existing
`POST /api/v1/push/tokens` and `DELETE /api/v1/push/tokens/:token`
endpoints — those are still how the app tells the server which Expo tokens
to send to. The new settings layer decides *whether* to send; the push
tokens decide *where*.

The `GET /api/v1/notifications` and `/badge` endpoints (notification
inbox/badge count) are also unchanged.

---

## Common UI flows

### Notifications & Settings screen

```ts
// Mount
const settings = await api.get('/api/v1/notification-settings');
setState(settings.data);

// User toggles "push notifications" off
await api.put('/api/v1/notification-settings', { push_enabled: false });

// User sets quiet hours to 10pm–7am
await api.put('/api/v1/notification-settings', {
  quiet_hours_enabled: true,
  quiet_hours_start: "22:00",
  quiet_hours_end:   "07:00",
});

// User picks "daily digest at 9am"
await api.put('/api/v1/notification-settings', {
  digest_frequency:     "daily",
  digest_delivery_hour: 9,
});

// User mutes everything for 24 hours
const muteUntil = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
await api.put('/api/v1/notification-settings', { global_mute_until: muteUntil });

// User taps "unmute"
await api.put('/api/v1/notification-settings', { clear_global_mute: true });
```

### Friends & Accountability screen — per-friend customization

```ts
// Mount: fetch settings + the user's overrides
const [settings, overrides] = await Promise.all([
  api.get('/api/v1/notification-settings'),
  api.get('/api/v1/notification-settings/friends'),
]);

// If apply_to_all_friends is true, show the shared config UI.
// If false, show the per-friend list with override status badges.

// User turns "Apply same to all" off and customizes Mike's settings
await api.put('/api/v1/notification-settings', { apply_to_all_friends: false });

await api.put(`/api/v1/notification-settings/friends/${mikeId}`, {
  prefs: {
    falling_behind:    { enabled: true, delivery: "immediate" },
    milestone_at_risk: { enabled: true, delivery: "immediate" },
  },
});

// User mutes Paul for 7 days
const muteUntil = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString();
await api.put(`/api/v1/notification-settings/friends/${paulId}`, {
  prefs: {},
  mute_until: muteUntil,
});

// User removes Mike's customization
await api.delete(`/api/v1/notification-settings/friends/${mikeId}`);
```

### Anchoring screen — per-roadmap customization

Same patterns as friends, with `/anchored-goals/:roadmapid` instead of
`/friends/:friendid`. The roadmapid must be a roadmap the user is
actively anchoring (accepted `AnchorRequest`).

```ts
// User mutes notifications about a specific anchored goal
await api.put(`/api/v1/notification-settings/anchored-goals/${roadmapId}`, {
  prefs: {},
  mute_until: muteUntil,
});
```

---

## Error reference

| Status | When | Body shape |
|---|---|---|
| `400 Bad Request` | Validation failed (bad enum, bad UUID, bad HH:MM, out-of-range integer, malformed delivery mode, self-targeted override) | `{ "error": "Bad Request", "message": "<detail>", "code": 400 }` |
| `401 Unauthorized` | Missing or invalid Bearer token | `{ "error": "Auth header missing" }` |
| `403 Forbidden` | Caller is not friends with the target user (per-friend override), or not the accepted anchor of the roadmap (per-anchored-goal override) | `{ "error": "Forbidden", "message": "<detail>", "code": 403 }` |
| `404 Not Found` | GET on an override that doesn't exist | `{ "error": "Not Found", "message": "notification override not found", "code": 404 }` |
| `500 Internal Server Error` | Database failure | `{ "error": "Internal Server Error", "message": "Internal server error: <detail>", "code": 500 }` |

The 400 error messages are human-readable; safe to surface to the user as a
toast/banner. The 500 messages should be treated as opaque ("Something went
wrong, please try again").

---

## Open product questions (frontend can defer building these)

These features are in the spec but not yet in the API surface — the UI can
be designed but not implemented until the corresponding backend step lands:

| Feature | API status | Recommended UI approach |
|---|---|---|
| Falling-behind triggers | Pref exists; detector lands in Step 8 | Show toggle as functional now; it'll start firing when Step 8 deploys |
| Quiet-hours deferred delivery | Pushes during quiet hours currently drop | UI can fully support quiet hours; behavior improves in Step 9 |
| Digest delivery (daily/weekly) | Pref exists; composer lands in Step 10 | Show toggle as functional now; effectively a no-op until Step 10 |
| Email channel | Toggle exists; provider not integrated | Hide the email row in v1 OR show as "coming soon" |
| Frequency caps | Live server-side (10 immediate pushes / 24h, not user-tunable) | No UI needed; `message_new` is exempt |
| `friend_accepted` / `_rejected`, `anchor_rejected` | Now gated — ride the originating request's toggle (see "What gets gated today") | Don't add separate toggles; they follow `friend_request` / `anchor_accepted_removed` |
| Nudges, messages, agent updates | Live — `nudges`, `messages`, `agent_updates` toggles | Surface these three toggles in settings |

---

## Reference

- Backend implementation: `notifications/` package, `api/handlers/notification_settings.go`, `database/notification_settings.go`
- Schema: `supabase/migrations/20260518000000_add_timezone_to_users.sql` + `20260518000001_add_notification_settings_tables.sql`
- Resolver logic (the cascade that decides whether to send): `notifications/resolver.go`
- Dispatch (the central send path): `notifications/dispatch.go`
- Cron scheduler scaffold: `main.go` `setupScheduler` + `notifications/jobs/`
- Existing API doc: `api/README.md` (everything else — auth, friends, roadmaps, etc.)
