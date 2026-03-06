# Nudge Feature Implementation

## Overview

The nudge feature allows accountability partners (anchors) to send lightweight "hey, I'm watching" signals to roadmap/habit owners. This reinforces accountability through presence without introducing task policing.

## Database

### Table: `nudges`

| Column | Type | Description |
|--------|------|-------------|
| nudge_id | UUID PK | Primary key |
| sender_id | UUID FK | The anchor sending the nudge |
| receiver_id | UUID FK | The roadmap/habit owner |
| target_type | VARCHAR | "roadmap" or "habit" |
| target_id | UUID | roadmap_id or habit_id |
| created_at | TIMESTAMP | For rate limiting + history |

### Migration

Run: `supabase db push` or apply `supabase/migrations/20260305000000_add_nudges_table.sql`

---

## API Endpoints

### Send Nudges

#### POST `/api/v1/roadmaps/:roadmapid/nudge`

Send a nudge to a roadmap owner. Caller must be the anchor.

**Response 200:**
```json
{
  "nudge_id": "uuid",
  "message": "Nudge sent successfully"
}
```

**Response 403 (not anchor):**
```json
{
  "error": "You are not the anchor for this roadmap"
}
```

**Response 429 (rate limited):**
```json
{
  "error": "Rate limit exceeded. Max 2 nudges per day per roadmap."
}
```

---

#### POST `/api/v1/habits/:habitid/nudge`

Send a nudge to a habit owner. Caller must be the anchor.

Same response format as roadmap nudge.

---

### View Nudges

#### GET `/api/v1/nudges?limit=20`

Get nudges received by the current user.

**Response:**
```json
{
  "nudges": [
    {
      "nudge_id": "uuid",
      "sender": {
        "user_id": "uuid",
        "first_name": "John",
        "last_name": "Doe"
      },
      "target_type": "roadmap",
      "target_id": "uuid",
      "target_title": "Learn Spanish",
      "created_at": "2024-01-15T10:30:00Z"
    }
  ]
}
```

---

#### GET `/api/v1/nudges/sent?limit=20`

Get nudges sent by the current user.

**Response:**
```json
{
  "nudges": [
    {
      "nudge_id": "uuid",
      "receiver": {
        "user_id": "uuid",
        "first_name": "Jane",
        "last_name": "Smith"
      },
      "target_type": "habit",
      "target_id": "uuid",
      "target_title": "Daily Exercise",
      "created_at": "2024-01-15T10:30:00Z"
    }
  ]
}
```

---

## Rate Limiting

- **Limit:** 2 nudges per day per target (per sender)
- **Window:** Rolling 24-hour window
- **Scope:** Per (sender, target_type, target_id) combination

Example: John can nudge Sarah's "Learn Spanish" roadmap 2x/day, AND her "Exercise" habit 2x/day separately.

---

## Push Notifications

When a nudge is sent, the receiver gets a push notification:

| Field | Value |
|-------|-------|
| Type | `"roadmap_nudge"` or `"habit_nudge"` |
| Title | `"Nudge from John"` |
| Body | `"John nudged you on 'Learn Spanish'"` |
| Data | `{ "type": "roadmap_nudge", "roadmap_id": "...", "sender_id": "...", "nudge_id": "..." }` |

---

## Files Changed

| File | Description |
|------|-------------|
| `types/types.go` | Added `Nudge`, `NudgeResponse` structs + constants + errors |
| `database/nudges.go` | CRUD operations + rate limit checking |
| `api/handlers/nudges.go` | HTTP handlers for all nudge endpoints |
| `api/routes/routes.go` | Registered 4 new endpoints |
| `supabase/migrations/20260305000000_add_nudges_table.sql` | Database migration |

---

## Frontend Integration

### Sending a Nudge

From the partner's roadmap view (where you're the anchor):

```typescript
// Nudge button click
const response = await api.post(`/api/v1/roadmaps/${roadmapId}/nudge`);

if (response.status === 200) {
  showToast("Nudge sent!");
} else if (response.status === 429) {
  showToast("You've already nudged twice today");
} else if (response.status === 403) {
  showToast("You're not the anchor for this roadmap");
}
```

### Viewing Received Nudges

```typescript
const response = await api.get('/api/v1/nudges?limit=10');
const nudges = response.data.nudges;

// Display in a list or notification center
nudges.forEach(nudge => {
  console.log(`${nudge.sender.first_name} nudged you on "${nudge.target_title}"`);
});
```

### Deep Linking from Notification

When user taps a nudge notification, navigate to the relevant roadmap/habit:

```typescript
// In notification handler
if (notification.data.type === 'roadmap_nudge') {
  navigation.navigate('RoadmapDetails', {
    roadmapId: notification.data.roadmap_id
  });
} else if (notification.data.type === 'habit_nudge') {
  navigation.navigate('HabitDetails', {
    habitId: notification.data.habit_id
  });
}
```
