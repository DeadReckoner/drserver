# Roadmap Lifecycle Controls API

This document describes the new lifecycle management and sharing features for roadmaps.

## Table of Contents
- [Overview](#overview)
- [Lifecycle Controls](#lifecycle-controls)
  - [Restart Roadmap](#restart-roadmap)
  - [Shift Dates](#shift-dates)
  - [Pause Roadmap](#pause-roadmap)
  - [Resume Roadmap](#resume-roadmap)
  - [Archive Roadmap](#archive-roadmap)
  - [Restore Roadmap](#restore-roadmap)
- [Sharing](#sharing)
  - [Generate Share Link](#generate-share-link)
  - [Revoke Share Link](#revoke-share-link)
  - [Get Shared Roadmap](#get-shared-roadmap-public)
  - [Copy Shared Roadmap](#copy-shared-roadmap)
- [Step Status](#step-status)
- [Listing Roadmaps](#listing-roadmaps-archived-filter)
- [New Roadmap Fields](#new-roadmap-fields)
- [Error Handling](#error-handling)

---

## Overview

The lifecycle controls allow users to:
- **Restart** a roadmap from today (reset all progress)
- **Shift dates** forward or backward by N days
- **Pause/Resume** a roadmap (freezes progress, resume shifts dates forward)
- **Archive/Restore** roadmaps (soft delete from active views)
- **Share** roadmaps via a link that others can copy to their account

---

## Lifecycle Controls

### Restart Roadmap

Resets a roadmap to start fresh from today. All completion states are cleared, and dates are recalculated based on the current date.

**Endpoint:** `PUT /api/v1/roadmaps/:roadmapid/restart`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "uuid",
    "goal": "Learn Guitar",
    "start_date": "2026-04-26T00:00:00Z",
    "completed": false,
    "paused_at": null,
    "phases": [...],
    "milestones": [...]
  }
}
```

**Preconditions:**
- Roadmap must have been started (`start_date != null`)

**What happens:**
1. Sets `start_date` to today
2. Sets `completed = false`
3. Clears `paused_at`
4. Resets all milestones: `reached = false`
5. Resets all tasks: `completed = false`
6. Resets all steps: `status = "incomplete"`, `completed = false`
7. Recalculates all milestone and task dates based on their `day` offset from new start date
8. Recomputes all statuses

**Errors:**
- `404` - Roadmap not found
- `400` - "roadmap has not been started"

---

### Shift Dates

Moves all dates in a roadmap forward or backward by a specified number of days.

**Endpoint:** `POST /api/v1/roadmaps/:roadmapid/shift`

**Headers:**
```
Authorization: Bearer <token>
Content-Type: application/json
```

**Request Body:**
```json
{
  "days": 7
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `days` | integer | Yes | Number of days to shift. Positive = forward, negative = backward |

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "uuid",
    "start_date": "2026-05-03T00:00:00Z",
    "phases": [...],
    "milestones": [...]
  }
}
```

**Preconditions:**
- Roadmap must have been started

**What happens:**
1. Shifts `start_date` by N days
2. Shifts all milestone dates by N days
3. Shifts all task dates by N days
4. Recomputes statuses (tasks may become overdue or not based on new dates)

**Errors:**
- `404` - Roadmap not found
- `400` - "roadmap has not been started"

---

### Pause Roadmap

Freezes a roadmap by recording when it was paused. The roadmap remains visible but dates are frozen.

**Endpoint:** `POST /api/v1/roadmaps/:roadmapid/pause`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "message": "Roadmap paused successfully"
}
```

**Preconditions:**
- Roadmap must have been started
- Roadmap must not already be paused

**What happens:**
1. Sets `paused_at` to current timestamp

**Errors:**
- `404` - Roadmap not found
- `400` - "roadmap has not been started"
- `400` - "roadmap is already paused"

---

### Resume Roadmap

Resumes a paused roadmap. Automatically shifts all dates forward by the duration it was paused.

**Endpoint:** `POST /api/v1/roadmaps/:roadmapid/resume`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "uuid",
    "paused_at": null,
    "start_date": "2026-05-03T00:00:00Z",
    "phases": [...],
    "milestones": [...]
  }
}
```

**Preconditions:**
- Roadmap must be paused (`paused_at != null`)

**What happens:**
1. Calculates days paused: `today - paused_at` (minimum 1 day)
2. Clears `paused_at`
3. Shifts all dates forward by the paused duration

**Example:**
- Roadmap paused on April 20
- Resume on April 26 (6 days later)
- All dates shift forward by 6 days

**Errors:**
- `404` - Roadmap not found
- `400` - "roadmap is not paused"

---

### Archive Roadmap

Soft-deletes a roadmap from active views. Archived roadmaps are hidden by default but can be restored.

**Endpoint:** `POST /api/v1/roadmaps/:roadmapid/archive`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "message": "Roadmap archived successfully"
}
```

**What happens:**
1. Sets `archived = true`

**Notes:**
- Idempotent: calling multiple times is safe
- Archived roadmaps are excluded from `GET /api/v1/roadmaps` by default

**Errors:**
- `404` - Roadmap not found

---

### Restore Roadmap

Restores an archived roadmap back to active status.

**Endpoint:** `POST /api/v1/roadmaps/:roadmapid/restore`

**Headers:**
```
Authorization: Bearer <token>
Content-Type: application/json
```

**Request Body (optional):**
```json
{
  "start_from_today": true
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `start_from_today` | boolean | No | If `true`, also restarts the roadmap from today (resets progress) |

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "uuid",
    "archived": false,
    "phases": [...],
    "milestones": [...]
  }
}
```

**Preconditions:**
- Roadmap must be archived

**What happens:**
1. Sets `archived = false`
2. If `start_from_today = true` AND roadmap was previously started, also performs a full restart

**Errors:**
- `404` - Roadmap not found
- `400` - "roadmap is not archived"

---

## Sharing

### Generate Share Link

Creates a unique share token for a roadmap that can be shared with others.

**Endpoint:** `POST /api/v1/roadmaps/:roadmapid/share`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "data": {
    "share_token": "abc123xyz..."
  }
}
```

**What happens:**
1. Generates a secure random token (32 characters, URL-safe base64)
2. Stores it in the `share_token` column
3. Calling again generates a new token (invalidates old one)

**Building the share URL:**
```
https://yourapp.com/shared/{share_token}
```

**Errors:**
- `404` - Roadmap not found

---

### Revoke Share Link

Removes the share token, making the roadmap no longer accessible via the share link.

**Endpoint:** `DELETE /api/v1/roadmaps/:roadmapid/share`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "message": "Share link revoked successfully"
}
```

**What happens:**
1. Sets `share_token = null`

**Errors:**
- `404` - Roadmap not found

---

### Get Shared Roadmap (Public)

Retrieves a shared roadmap by its share token. **No authentication required.**

**Endpoint:** `GET /api/v1/roadmaps/shared/:token`

**Headers:** None required

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "uuid",
    "goal": "Learn Guitar",
    "description": "A 30-day guitar learning roadmap",
    "days": 30,
    "type": "ai_generated",
    "phases": [...],
    "milestones": [...]
  }
}
```

**Privacy:**
The following fields are stripped from the response:
- `user_id` (empty string)
- `anchor_id` (null)
- `share_token` (null)

**Errors:**
- `404` - "Shared roadmap not found"

---

### Copy Shared Roadmap

Creates a copy of a shared roadmap in the authenticated user's account.

**Endpoint:** `POST /api/v1/roadmaps/shared/:token/copy`

**Headers:**
```
Authorization: Bearer <token>
```

**Request Body:** None

**Response (201 Created):**
```json
{
  "data": {
    "roadmap_id": "new-uuid",
    "goal": "Learn Guitar",
    "start_date": null,
    "completed": false,
    "phases": [...],
    "milestones": [...]
  }
}
```

**What happens:**
1. Creates a deep copy of the entire roadmap structure:
   - Roadmap
   - Phases
   - Milestones (including spikes, risks)
   - Tasks
   - Steps
2. Assigns new UUIDs to all entities
3. Sets `user_id` to the authenticated user
4. Resets to unstarted state:
   - `start_date = null`
   - `completed = false`
   - All milestones: `reached = false`, `date = null`
   - All tasks: `completed = false`, `date = null`
   - All steps: `status = "incomplete"`, `completed = false`
5. Clears sharing/lifecycle fields:
   - `share_token = null`
   - `paused_at = null`
   - `archived = false`
   - `anchor_id = null`

**The copy is completely independent of the original.**

**Errors:**
- `404` - "Shared roadmap not found"

---

## Step Status

Steps now have a `status` field with three possible values:

| Status | Description | Blocks Task Completion? |
|--------|-------------|------------------------|
| `incomplete` | Step not done (default) | Yes |
| `completed` | Step finished | No |
| `skipped` | Step intentionally skipped | No |

### Updating Step Status

**Endpoint:** `PATCH /api/v1/tasks/:taskid/steps/:stepid`

**Request Body:**
```json
{
  "status": "skipped"
}
```

Or using the legacy boolean field (still supported):
```json
{
  "completed": true
}
```

**Behavior:**
- Setting `status = "completed"` automatically sets `completed = true`
- Setting `status = "incomplete"` or `"skipped"` sets `completed = false`
- Setting `completed = true` automatically sets `status = "completed"`
- Setting `completed = false` automatically sets `status = "incomplete"`

### Task Auto-Completion

A task is automatically marked complete when **all steps are either `completed` or `skipped`**.

**Example:**
```
Step 1: status = "completed"  ✓
Step 2: status = "skipped"    ✓ (counts as done)
Step 3: status = "incomplete" ✗ (blocks completion)
```
Task is NOT auto-completed because Step 3 is incomplete.

```
Step 1: status = "completed"  ✓
Step 2: status = "skipped"    ✓
Step 3: status = "completed"  ✓
```
Task IS auto-completed.

---

## Listing Roadmaps (Archived Filter)

The existing endpoint now supports filtering archived roadmaps:

**Endpoint:** `GET /api/v1/roadmaps`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `include_archived` | string | `"false"` | Set to `"true"` to include archived roadmaps |

**Examples:**
```
GET /api/v1/roadmaps                         # Active roadmaps only
GET /api/v1/roadmaps?include_archived=true   # All roadmaps including archived
```

---

## New Roadmap Fields

The `Roadmap` object now includes these additional fields:

```json
{
  "roadmap_id": "uuid",
  "goal": "Learn Guitar",
  "archived": false,
  "paused_at": null,
  "share_token": "abc123...",
  ...
}
```

| Field | Type | Description |
|-------|------|-------------|
| `archived` | boolean | Whether the roadmap is archived (default: `false`) |
| `paused_at` | string (ISO 8601) or null | Timestamp when paused, null if not paused |
| `share_token` | string or null | Share token if sharing is enabled, null otherwise |

**Note:** `share_token` is only returned to the roadmap owner, not in shared views.

---

## Error Handling

All endpoints return errors in this format:

```json
{
  "error": "Error message here"
}
```

### Common HTTP Status Codes

| Code | Meaning |
|------|---------|
| `200` | Success |
| `201` | Created (for copy endpoint) |
| `400` | Bad request (precondition not met) |
| `401` | Unauthorized (missing/invalid token) |
| `404` | Resource not found |
| `500` | Internal server error |

### Common Error Messages

| Endpoint | Error | Meaning |
|----------|-------|---------|
| restart | "roadmap has not been started" | Roadmap's `start_date` is null |
| shift | "roadmap has not been started" | Roadmap's `start_date` is null |
| pause | "roadmap has not been started" | Roadmap's `start_date` is null |
| pause | "roadmap is already paused" | Roadmap's `paused_at` is not null |
| resume | "roadmap is not paused" | Roadmap's `paused_at` is null |
| restore | "roadmap is not archived" | Roadmap's `archived` is false |

---

## Example Flows

### Flow 1: User falls behind and wants to catch up

```
1. POST /api/v1/roadmaps/{id}/shift
   Body: { "days": 7 }

   Result: All dates shift forward 7 days, giving user more time
```

### Flow 2: User takes a vacation

```
1. POST /api/v1/roadmaps/{id}/pause
   Result: Roadmap paused at current time

... user returns from 2-week vacation ...

2. POST /api/v1/roadmaps/{id}/resume
   Result: All dates shift forward by ~14 days
```

### Flow 3: User wants to start over

```
1. PUT /api/v1/roadmaps/{id}/restart
   Result: All progress reset, dates recalculated from today
```

### Flow 4: User archives old roadmap, later restores it

```
1. POST /api/v1/roadmaps/{id}/archive
   Result: Roadmap hidden from list

... later ...

2. POST /api/v1/roadmaps/{id}/restore
   Body: { "start_from_today": true }
   Result: Roadmap restored and reset to start today
```

### Flow 5: User shares roadmap with a friend

```
1. POST /api/v1/roadmaps/{id}/share
   Response: { "data": { "share_token": "abc123..." } }

2. User sends link to friend: https://app.com/shared/abc123...

3. Friend opens link (no auth needed):
   GET /api/v1/roadmaps/shared/abc123...
   Response: Full roadmap structure

4. Friend copies to their account (requires auth):
   POST /api/v1/roadmaps/shared/abc123.../copy
   Response: New roadmap in friend's account
```

---

## Database Migration

The migration adds:

```sql
-- Roadmap fields
ALTER TABLE roadmaps ADD COLUMN archived boolean NOT NULL DEFAULT false;
ALTER TABLE roadmaps ADD COLUMN paused_at timestamp with time zone;
ALTER TABLE roadmaps ADD COLUMN share_token text UNIQUE;

-- Step status field
ALTER TABLE steps ADD COLUMN status text NOT NULL DEFAULT 'incomplete';
-- Constraint: status IN ('incomplete', 'completed', 'skipped')
```

Indexes created:
- `roadmaps_user_archived_idx` on `(user_id, archived)`
- `roadmaps_share_token_idx` on `share_token` (partial, where not null)
