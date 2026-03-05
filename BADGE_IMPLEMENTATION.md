# App Icon Badge Implementation

## Overview

The server now supports app icon badges that display a count of actionable items. Badge counts are automatically included with all push notifications and can be manually refreshed via API.

## What Counts Toward the Badge

The badge count is the sum of:

1. **Unread messages** - Messages from other users that haven't been read
2. **Incomplete habits due today** - Habits scheduled for today that aren't completed
3. **Incomplete tasks due today** - Tasks with today's date that aren't completed
4. **Pending friend requests** - Incoming friend requests awaiting response
5. **Pending anchor requests** - Incoming anchor requests awaiting response

---

## API Endpoints

### GET `/api/v1/badge`

Returns the current badge count without sending a push notification.

**Response:**
```json
{
  "badge_count": 5
}
```

**Use case:** Display badge count in the app UI, or check count on app launch.

---

### POST `/api/v1/badge/refresh`

Calculates the current badge count and sends a silent push notification to update the app icon badge on all user devices.

**Response:**
```json
{
  "badge_count": 3,
  "push_sent": true
}
```

Or if push fails (badge count is still returned):
```json
{
  "badge_count": 3,
  "push_sent": false,
  "message": "Badge count calculated but push update failed"
}
```

**Use case:** Call this after actions that change the badge count:
- Reading messages
- Completing a habit
- Completing a task
- Accepting/rejecting a friend request
- Accepting/rejecting an anchor request

---

## Automatic Badge Updates

All push notifications now automatically include the badge count. When a user receives any notification (new message, friend request, etc.), the badge will be updated to reflect the current count.

The Expo push payload now includes:
```json
{
  "to": "ExponentPushToken[xxx]",
  "title": "New Message",
  "body": "John sent you a message",
  "data": { ... },
  "sound": "default",
  "priority": "high",
  "badge": 5
}
```

---

## Mobile App Integration

### iOS Setup

Expo handles badge permissions automatically. Ensure your app.json includes:

```json
{
  "expo": {
    "ios": {
      "supportsTablet": true
    },
    "plugins": [
      [
        "expo-notifications",
        {
          "icon": "./assets/notification-icon.png"
        }
      ]
    ]
  }
}
```

### Recommended Integration Points

1. **On app launch/resume:**
   ```typescript
   // Fetch current badge count to sync UI
   const response = await api.get('/api/v1/badge');
   // Optionally refresh to ensure badge is current
   await api.post('/api/v1/badge/refresh');
   ```

2. **After reading messages:**
   ```typescript
   // After marking messages as read
   await api.post('/api/v1/badge/refresh');
   ```

3. **After completing a habit:**
   ```typescript
   await api.post(`/api/v1/habits/${habitId}/complete`);
   await api.post('/api/v1/badge/refresh');
   ```

4. **After completing a task:**
   ```typescript
   await api.patch(`/api/v1/tasks/${taskId}`, { completed: true });
   await api.post('/api/v1/badge/refresh');
   ```

5. **After accepting/rejecting friend or anchor requests:**
   ```typescript
   await api.patch(`/api/v1/friends/${id}`, { status: 'accepted' });
   await api.post('/api/v1/badge/refresh');
   ```

### Clearing the Badge Manually (Optional)

If you need to clear the badge when the user opens the app (common UX pattern), you can use Expo's API:

```typescript
import * as Notifications from 'expo-notifications';

// Clear badge when app becomes active
Notifications.setBadgeCountAsync(0);
```

However, this may conflict with the server-sent badge count. A better approach is to call `/api/v1/badge/refresh` which will send the accurate count.

---

## Silent Push Notifications

The `/api/v1/badge/refresh` endpoint sends a "content-available" silent push that updates the badge without showing a notification to the user. This is handled automatically by Expo/iOS.

---

## Testing

1. Register a push token via `POST /api/v1/push/tokens`
2. Create some unread messages or incomplete habits
3. Call `GET /api/v1/badge` to verify the count
4. Call `POST /api/v1/badge/refresh` to update the app icon
5. Check the app icon on the home screen for the badge

---

## Files Changed

- `database/badges.go` - Badge count calculation logic
- `api/utils/notifications.go` - Badge included in push payloads + refresh functions
- `api/handlers/notifications.go` - New badge endpoints
- `api/routes/routes.go` - New routes for `/api/v1/badge`
