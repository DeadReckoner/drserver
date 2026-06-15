# Reporting & Blocking — Frontend Integration Guide

Backend API for App Store UGC safety (Guideline 1.2): reporting other users or
AI-generated content, and "Remove & Block". This is what the mobile client wires
up. Reporting and blocking are **independent** — reporting never removes or
blocks anyone, and blocking never files a report.

- Base URL: `/api/v1`
- Auth: every endpoint requires the Supabase JWT — `Authorization: Bearer <token>`.
  The reporter / blocker is derived from the token, never sent in the body.
- Content type: `application/json`.

---

## 1. Submit a report

```
POST /api/v1/reports
```

One endpoint for both report kinds. The `type` field decides which other fields
are required.

### Request body

| Field | Type | Required | Notes |
|---|---|---|---|
| `type` | `"user"` \| `"ai"` | yes | Selects the contract below. |
| `reason` | string | yes | Must be one of the reasons for the chosen `type` (see below). |
| `note` | string | no | Free-text detail from the user. Max 2000 chars. |
| **`type: "user"`** | | | |
| `reported_user_id` | UUID | yes (user) | The member being reported. |
| `thread_id` | UUID | no | Message thread reference, if reporting from a conversation. |
| `message_id` | UUID | no | Specific message reference. |
| **`type: "ai"`** | | | |
| `output_type` | string | yes (ai) | Which AI surface — see conventional values below. |
| `output_id` | UUID | no\* | ID of the AI output entity, when it exists. |
| `roadmap_id` | UUID | no | Goal/roadmap the output belongs to, if applicable. |
| `job_id` | UUID | no | Generation job reference, if applicable. |
| `context` | object | no\* | Snapshot of the reported content (see below). |
| **Client metadata (either type)** | | | |
| `app_version` | string | no | e.g. `"1.4.0"`. |
| `os` | string | no | e.g. `"ios"`. |
| `device` | string | no | e.g. `"iPhone 16"`. |

\* For `type: "ai"` you must send **either `output_id` or a non-empty `context`**.
AI previews (an unaccepted roadmap, a chat suggestion, an accountability message)
often aren't persisted yet, so when there's no stable `output_id`, put a snapshot
of exactly what the user was looking at in `context`. That snapshot is what makes
the report reviewable.

### Reasons (must match the `type`)

| `type: "user"` | `type: "ai"` |
|---|---|
| `harassment` | `unsafe` |
| `spam` | `inaccurate` |
| `inappropriate_content` | `offensive` |
| `impersonation` | `medical_mental_health` |
| `other` | `legal_financial` |
| | `other` |

### `output_type` conventional values

Free-form string, but please use these so reports group cleanly:
`roadmap`, `milestone`, `task`, `habit_suggestion`, `roadmap_revision`,
`accountability_message`, `other`.

### `context` snapshot

An arbitrary JSON object — send whatever the user is seeing so support can review
it. Suggested shape:

```json
{
  "surface": "roadmap_preview",
  "text": "Full text of the AI output the user is reporting…",
  "extra": { "milestone_title": "Week 1: Fast for 72 hours" }
}
```

### Responses

`201 Created` — body is the created report. **This is the submission
confirmation** (show your success toast on `201`):

```json
{
  "report_id": "9f1c…",
  "reporter_id": "u-123",
  "type": "ai",
  "reason": "unsafe",
  "note": "Suggested an extreme fast",
  "status": "open",
  "output_type": "roadmap",
  "roadmap_id": "r-456",
  "context": { "surface": "roadmap_preview", "text": "…" },
  "created_at": "2026-06-15T18:20:00Z"
}
```

Errors use this shape:

```json
{ "error": "Bad Request", "message": "reason is not valid for this report type", "code": 400 }
```

| Status | When | `message` (examples) |
|---|---|---|
| 400 | Validation failed | `type must be 'user' or 'ai'`, `reason is required`, `reason is not valid for this report type`, `reported_user_id is required for a user report`, `output_type is required for an AI report`, `an AI report requires either output_id or context`, `note is too long`, `you cannot report yourself` |
| 401 | Missing/invalid token | (from auth middleware) |
| 404 | `reported_user_id` doesn't exist | `Reported user not found` |
| 429 | More than 10 reports/hour from this user | `too many reports submitted; please try again later` |
| 500 | Server error | `Internal server error: …` |

### Examples

User report (from a message):

```bash
curl -X POST https://<host>/api/v1/reports \
  -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
  -d '{
    "type": "user",
    "reason": "harassment",
    "note": "Repeated abusive messages",
    "reported_user_id": "11111111-1111-1111-1111-111111111111",
    "thread_id": "22222222-2222-2222-2222-222222222222",
    "message_id": "33333333-3333-3333-3333-333333333333"
  }'
```

AI report (unsaved roadmap preview):

```ts
await fetch(`${API}/api/v1/reports`, {
  method: "POST",
  headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
  body: JSON.stringify({
    type: "ai",
    reason: "unsafe",
    output_type: "roadmap",
    // no output_id yet — preview isn't saved, so snapshot it:
    context: { surface: "roadmap_preview", text: previewText },
    app_version: appVersion,
    os: Platform.OS,
  }),
});
// 201 → show "Report submitted" confirmation
```

---

## 2. Remove & Block

`:id` here is the **other user's `user_id`** (same as the existing Remove call),
not a friendship id.

```
POST   /api/v1/friends/:id/block     # Remove & Block (atomic)
DELETE /api/v1/friends/:id/block     # Unblock (lets you re-invite them)
DELETE /api/v1/friends/:id           # Plain Remove (existing, unchanged)
```

- **Remove** ends the friendship, shared access, and messaging.
- **Remove & Block** does all of that *and* records a block, so the other person
  can't send you friend requests or messages. It's idempotent and works even if
  you aren't currently friends.
- **Unblock** lifts a block you placed; only then can you re-invite that user via
  `POST /api/v1/friends/request/:id`. The blocked user can never unblock
  themselves.

### What a block prevents

| Action by the other (blocked) user | Result |
|---|---|
| `POST /api/v1/friends/request/:id` (friend request / re-invite) | `403` — `cannot send friend request to this user` |
| Sending a message | Rejected (the thread is also deleted on block) |

The block is mutual in effect (neither party can contact the other) but only the
blocker owns it.

### Responses

All return the simple message envelope `{ "message": "…" }`:

| Endpoint | Success | Notes |
|---|---|---|
| `POST /friends/:id/block` | `200 {"message":"Friend removed and blocked"}` | `400 {"message":"cannot block yourself"}` if `:id` is the caller. |
| `DELETE /friends/:id/block` | `200 {"message":"User unblocked"}` | No-op if not blocked. |
| `DELETE /friends/:id` | `200 {"message":"Friendship removed successfully"}` | `404` if no friendship. |

`:id` must be a valid UUID, else `400 Invalid id: must be a valid UUID`.

```bash
# Remove & Block
curl -X POST https://<host>/api/v1/friends/11111111-1111-1111-1111-111111111111/block \
  -H "Authorization: Bearer $JWT"

# Unblock (so you can re-invite later)
curl -X DELETE https://<host>/api/v1/friends/11111111-1111-1111-1111-111111111111/block \
  -H "Authorization: Bearer $JWT"
```

---

## 3. Where to surface these (from the product ticket)

- **Report a user** — add a "Report" action next to the existing Remove on an
  Anchor/user, and on an individual message. Picking it opens reason + optional
  note → `POST /reports` with `type: "user"` (include `thread_id`/`message_id`
  when reporting from a conversation).
- **Remove & Block** — third option on the Remove Friend confirmation modal
  (Remove / Remove & Block / Cancel) → `POST /friends/:id/block`.
- **Report AI output** — in the overflow menu (not a persistent button) on the
  AI roadmap preview, and on any other AI surface (task/habit suggestions,
  revisions, accountability messages) → `POST /reports` with `type: "ai"`,
  the right `output_type`, and a `context` snapshot when the output isn't saved.

A `201` (report) or `200` (block) is your cue to show the user a confirmation.
