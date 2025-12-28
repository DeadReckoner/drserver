# Push Notifications Implementation

## Overview
Push notifications have been implemented for messages, friend requests, and anchor requests using the centralized notification utility in `api/utils/notifications.go`.

## Notification Types

### Messages
- **`message_new`**: Sent when a new message is received in a thread
  - Sent to: All thread participants except the sender
  - Data includes: `thread_id`, `message_id`
  - Title: Sender's full name
  - Body: Message content (truncated to 100 characters if longer)

### Friend Requests
- **`friend_request`**: Sent when a friend request is created
  - Sent to: The addressee (person being requested)
  - Data includes: `friendship_id`, `requester_id`
  - Title: "New Friend Request"
  - Body: "{Requester Name} wants to connect with you"

- **`friend_accepted`**: Sent when a friend request is accepted
  - Sent to: The requester (person who sent the request)
  - Data includes: `friendship_id`, `addressee_id`
  - Title: "Friend Request Accepted"
  - Body: "{Addressee Name} accepted your friend request"

- **`friend_rejected`**: Sent when a friend request is rejected
  - Sent to: The requester
  - Data includes: `friendship_id`, `addressee_id`
  - Title: "Friend Request Declined"
  - Body: "{Addressee Name} declined your friend request"

### Anchor Requests
- **`anchor_request`**: Sent when an anchor request is created
  - Sent to: The anchor (person being requested to be anchor)
  - Data includes: `request_id`, `roadmap_id`
  - Title: "New Anchor Request"
  - Body: "{Requester Name} wants you to be their anchor"

- **`anchor_accepted`**: Sent when an anchor request is accepted
  - Sent to: The requester
  - Data includes: `request_id`, `roadmap_id`
  - Title: "Anchor Request Accepted"
  - Body: "{Anchor Name} accepted your anchor request"

- **`anchor_rejected`**: Sent when an anchor request is rejected
  - Sent to: The requester
  - Data includes: `request_id`, `roadmap_id`
  - Title: "Anchor Request Declined"
  - Body: "{Anchor Name} declined your anchor request"

### Roadmap Generation (Already Implemented)
- **`roadmap_complete`**: Sent when roadmap generation completes successfully
- **`roadmap_failed`**: Sent when roadmap generation fails

## Files Modified

### 1. `/api/utils/notifications.go` (New File)
Central notification utility with:
- `SendPushNotification()` - Main public function
- `sendExpoNotification()` - Expo API integration
- `updateNotificationStatus()` - Database status updates

### 2. `/api/handlers/messages.go`
- Added `sendMessageNotifications()` helper function
- Integrated notification call in WebSocket message handler

### 3. `/api/handlers/friends.go`
- Added `sendFriendRequestNotification()` for new requests
- Added `sendFriendStatusNotification()` for accept/reject
- Integrated calls in `CreateFriendRequest()` and `UpdateFriendRequestStatus()`

### 4. `/api/handlers/roadmap.go`
- Added `sendAnchorRequestNotification()` for new requests
- Added `sendAnchorStatusNotification()` for accept/reject
- Integrated calls in `CreateAnchorRequest()`, `AcceptAnchorRequest()`, and `RejectAnchorRequest()`

### 5. `/api/handlers/agent.go`
- Updated to use centralized `utils.SendPushNotification()`
- Removed duplicate notification code

### 6. `/database/user.go`
- Added `GetUserByID()` helper function for retrieving user information

## How It Works

1. **Registration**: Mobile clients register their Expo push tokens via `POST /api/v1/notifications/push-token`

2. **Event Trigger**: When an event occurs (message sent, request created, etc.), the handler calls the appropriate notification function

3. **Notification Creation**:
   - A notification record is created in the database
   - All active push tokens for the recipient are retrieved

4. **Push Delivery**:
   - Notifications are sent to Expo's push service in parallel (goroutines)
   - Status is updated in the database (sent/failed)

5. **Client Handling**: Mobile app receives the push notification with the data payload and can navigate to the appropriate screen

## Testing

Test notifications by:
1. Registering a push token from the mobile app
2. Triggering events (send message, create friend request, etc.)
3. Verifying push notifications appear on the device
4. Checking notification status in the database

## Future Enhancements

Potential additions:
- Notification preferences/settings per user
- Batch notifications for multiple events
- Rich media notifications (images, actions)
- Custom notification sounds per type
- Notification history/archive
