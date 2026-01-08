# DR Server API Documentation

## Base URL
```
http://localhost:8080
```

## Authentication
All API endpoints (except health checks) require authentication using Bearer tokens.

**Header Format:**
```
Authorization: Bearer <your_access_token>
```

The API uses Supabase authentication. The server validates tokens by making requests to the Supabase auth service.

**Error Responses for Authentication:**
- `401 Unauthorized` - Missing or invalid authorization header
- Token validation is performed by calling Supabase auth endpoint

## Health Check Endpoints

### GET /health
Basic health check for the service.

**Response:**
```json
{
  "status": "ok",
  "service": "dr-server"
}
```

### GET /healthz
Simplified health check endpoint.

**Response:**
```json
{
  "status": "ok"
}
```

---

## User Management

### POST /api/v1/users
Create a new user.

**Request Body:**
```json
{
  "first_name": "John",
  "last_name": "Doe",
  "email": "john.doe@example.com",
  "phone_number": "+1-555-123-4567"
}
```

**Response (200 OK):**
```json
{
  "data": {
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "first_name": "John",
    "last_name": "Doe",
    "email": "john.doe@example.com",
    "phone_number": "+1-555-123-4567",
    "CreatedAt": "2024-01-01T10:00:00Z"
  }
}
```

### GET /api/v1/users
Get current authenticated user information.

**Response (200 OK):**
```json
{
  "data": {
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "first_name": "John",
    "last_name": "Doe",
    "email": "john.doe@example.com",
    "phone_number": "+1-555-123-4567",
    "CreatedAt": "2024-01-01T10:00:00Z"
  }
}
```

**Response (404 Not Found):**
```json
{
  "error": "User not found"
}
```

### PATCH /api/v1/users/:id
Update user information.

**URL Parameters:**
- `id` (UUID) - User ID

**Request Body:**
```json
{
  "first_name": "Jane",
  "last_name": "Smith",
  "email": "jane.smith@example.com",
  "phone_number": "+1-555-987-6543"
}
```

**Response (200 OK):**
```json
{
  "data": {
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "first_name": "Jane",
    "last_name": "Smith",
    "email": "jane.smith@example.com",
    "phone_number": "+1-555-987-6543",
    "CreatedAt": "2024-01-01T10:00:00Z"
  }
}
```

### DELETE /api/v1/users/:id
Delete a user.

**URL Parameters:**
- `id` (UUID) - User ID

**Response (200 OK):**
```json
{
  "message": "User deleted successfully"
}
```

### GET /api/v1/users/search
Search for users by email (used internally by friend search).

**Query Parameters:**
- `email` (required) - Email to search (partial match)

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/users/search?email=john"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "first_name": "John",
      "last_name": "Doe",
      "email": "john.doe@example.com",
      "phone_number": "+1-555-123-4567",
      "CreatedAt": "2024-01-01T10:00:00Z"
    }
  ]
}
```

**Note:** For friend-related searches with friendship status, use `/api/v1/friends/search` instead.

---

## Friend Management

### GET /api/v1/friends/search
Search for users by email with friendship status.

**Query Parameters:**
- `email` (required) - Email to search (partial match)

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/friends/search?email=john"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "first_name": "John",
      "last_name": "Doe",
      "email": "john.doe@example.com",
      "friendship_status": "none"
    },
    {
      "user_id": "550e8400-e29b-41d4-a716-446655440001",
      "first_name": "Jane",
      "last_name": "Smith",
      "email": "jane.smith@example.com",
      "friendship_status": "accepted"
    }
  ]
}
```

**Friendship Status Values:**
- `none` - No friendship relationship
- `pending` - Friend request exists (sent or received)
- `accepted` - Users are friends

### POST /api/v1/friends/request/:id
Send a friend request to another user.

**URL Parameters:**
- `id` (UUID) - Target user ID to send friend request to

**Example Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/friends/request/550e8400-e29b-41d4-a716-446655440001"
```

**Response (201 Created):**
```json
{
  "data": {
    "id": "friendship-uuid",
    "requester_id": "your-user-uuid",
    "addressee_id": "target-user-uuid",
    "status": "pending",
    "restrictions": null,
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": null
  }
}
```

**Error Responses:**
- `400 Bad Request` - Trying to friend yourself
- `404 Not Found` - Target user not found
- `409 Conflict` - Friend request already exists or users are already friends

### GET /api/v1/friends/requests
Get all pending friend requests received by you.

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/friends/requests"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "id": "friendship-uuid",
      "requester_id": "requester-user-uuid",
      "addressee_id": "your-user-uuid",
      "status": "pending",
      "restrictions": null,
      "created_at": "2024-01-01T10:00:00Z",
      "updated_at": null,
      "requester": {
        "user_id": "requester-user-uuid",
        "first_name": "Jane",
        "last_name": "Smith",
        "email": "jane.smith@example.com",
        "phone_number": "+1-555-987-6543",
        "CreatedAt": "2024-01-01T09:00:00Z"
      }
    }
  ]
}
```

### PATCH /api/v1/friends/:id
Accept or reject a friend request (only addressee can update).

**URL Parameters:**
- `id` (UUID) - Friendship ID

**Request Body:**
```json
{
  "status": "accepted"
}
```
or
```json
{
  "status": "rejected"
}
```

**Example Request (Accept):**
```bash
curl -X PATCH \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "accepted"}' \
  "http://localhost:8080/api/v1/friends/friendship-uuid"
```

**Response (200 OK):**
```json
{
  "data": {
    "id": "friendship-uuid",
    "requester_id": "requester-user-uuid",
    "addressee_id": "your-user-uuid",
    "status": "accepted",
    "restrictions": null,
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:30:00Z"
  }
}
```

**Error Responses:**
- `400 Bad Request` - Invalid status or request is not pending
- `403 Forbidden` - Only the addressee can respond to a friend request
- `404 Not Found` - Friendship not found

### GET /api/v1/friends
Get list of all your friends with their profile information.

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/friends"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "friendship_id": "friendship-uuid",
      "friend": {
        "user_id": "friend-user-uuid",
        "first_name": "Jane",
        "last_name": "Smith",
        "email": "jane.smith@example.com",
        "phone_number": "+1-555-987-6543",
        "CreatedAt": "2024-01-01T09:00:00Z"
      },
      "restrictions": {
        "allowed_roadmap_ids": ["roadmap-uuid-1", "roadmap-uuid-2"]
      },
      "created_at": "2024-01-01T10:00:00Z"
    }
  ]
}
```

### GET /api/v1/friends/:id/profile
Get a friend's profile with roadmaps where you are the accountability partner (anchor).

**URL Parameters:**
- `id` (UUID) - Friend's user ID

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/friends/550e8400-e29b-41d4-a716-446655440001/profile"
```

**Response (200 OK):**
```json
{
  "data": {
    "user": {
      "user_id": "friend-user-uuid",
      "first_name": "Jane",
      "last_name": "Smith",
      "email": "jane.smith@example.com",
      "phone_number": "+1-555-987-6543",
      "CreatedAt": "2024-01-01T09:00:00Z"
    },
    "allowed_roadmaps": [
      {
        "roadmap_id": "roadmap-uuid-1",
        "user_id": "friend-user-uuid",
        "goal": "Learn Go Programming",
        "description": "Master Go in 90 days",
        "days": 90,
        "completed": false,
        "start_date": "2024-01-01T00:00:00Z",
        "anchor_id": "your-user-uuid",
        "milestones": [...]
      }
    ]
  }
}
```

**Roadmap Visibility Rules:**
- Friends can only see roadmaps where they are the accountability partner (anchor)
- This ensures privacy while allowing accountability partners to track progress

**Error Responses:**
- `403 Forbidden` - You are not friends with this user

### DELETE /api/v1/friends/:id
Remove a friendship.

**URL Parameters:**
- `id` (UUID) - Friend's user ID

**Example Request:**
```bash
curl -X DELETE \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/friends/550e8400-e29b-41d4-a716-446655440001"
```

**Response (200 OK):**
```json
{
  "message": "Friendship removed successfully"
}
```

**Error Responses:**
- `404 Not Found` - Friendship not found

---

## Anchor Management (Accountability Partners)

Anchors are accountability partners who can view and track progress on a friend's roadmap. This feature enables users to request friends to help keep them accountable on their goals.

### POST /api/v1/roadmaps/:roadmapid/anchor-request
Send a request to a friend to be an accountability partner (anchor) for a specific roadmap.

**URL Parameters:**
- `roadmapid` (UUID) - Roadmap ID

**Request Body:**
```json
{
  "anchor_id": "friend-user-uuid"
}
```

**Example Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"anchor_id": "550e8400-e29b-41d4-a716-446655440001"}' \
  "http://localhost:8080/api/v1/roadmaps/roadmap-uuid/anchor-request"
```

**Response (201 Created):**
```json
{
  "data": {
    "request_id": "request-uuid",
    "roadmap_id": "roadmap-uuid",
    "requester_id": "your-user-uuid",
    "anchor_id": "friend-user-uuid",
    "status": "pending",
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": null,
    "roadmap": {
      "roadmap_id": "roadmap-uuid",
      "goal": "Learn Web Development",
      "description": "90-day roadmap",
      "days": 90,
      "completed": false,
      "start_date": "2024-01-01T00:00:00Z"
    },
    "requester": {...},
    "anchor": {...}
  }
}
```

**Error Responses:**
- `403 Forbidden` - You are not friends with this user
- `404 Not Found` - Roadmap not found or you don't have permission
- `409 Conflict` - Anchor request already exists or user is already an anchor

### GET /api/v1/anchor-requests
Get all pending anchor requests where you are the requested anchor.

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/anchor-requests"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "request_id": "request-uuid",
      "roadmap_id": "roadmap-uuid",
      "requester_id": "friend-user-uuid",
      "anchor_id": "your-user-uuid",
      "status": "pending",
      "created_at": "2024-01-01T10:00:00Z",
      "updated_at": null,
      "roadmap": {
        "roadmap_id": "roadmap-uuid",
        "goal": "Learn Web Development",
        "description": "90-day roadmap",
        "days": 90
      },
      "requester": {
        "user_id": "friend-user-uuid",
        "first_name": "Jane",
        "last_name": "Smith",
        "email": "jane.smith@example.com"
      }
    }
  ]
}
```

### POST /api/v1/anchor-requests/:requestid/accept
Accept an anchor request to become an accountability partner.

**URL Parameters:**
- `requestid` (UUID) - Anchor request ID

**Example Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/anchor-requests/request-uuid/accept"
```

**Response (200 OK):**
```json
{
  "data": {
    "request_id": "request-uuid",
    "roadmap_id": "roadmap-uuid",
    "requester_id": "friend-user-uuid",
    "anchor_id": "your-user-uuid",
    "status": "accepted",
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:30:00Z"
  }
}
```

**Error Responses:**
- `400 Bad Request` - Anchor request is not pending
- `403 Forbidden` - Only the anchor can accept this request
- `404 Not Found` - Anchor request not found

### POST /api/v1/anchor-requests/:requestid/reject
Reject an anchor request.

**URL Parameters:**
- `requestid` (UUID) - Anchor request ID

**Example Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/anchor-requests/request-uuid/reject"
```

**Response (200 OK):**
```json
{
  "data": {
    "request_id": "request-uuid",
    "roadmap_id": "roadmap-uuid",
    "requester_id": "friend-user-uuid",
    "anchor_id": "your-user-uuid",
    "status": "rejected",
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:30:00Z"
  }
}
```

**Error Responses:**
- `400 Bad Request` - Anchor request is not pending
- `403 Forbidden` - Only the anchor can reject this request
- `404 Not Found` - Anchor request not found

### DELETE /api/v1/roadmaps/:roadmapid/anchor
Remove the accountability partner from a roadmap (roadmap owner only).

**URL Parameters:**
- `roadmapid` (UUID) - Roadmap ID

**Example Request:**
```bash
curl -X DELETE \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/roadmaps/roadmap-uuid/anchor"
```

**Response (200 OK):**
```json
{
  "message": "Anchor removed successfully"
}
```

**Error Responses:**
- `400 Bad Request` - No anchor assigned to this roadmap
- `404 Not Found` - Roadmap not found or you don't have permission

**How Anchor System Works:**
1. User sends an anchor request to a friend for a specific roadmap
2. The system verifies they are friends (returns 403 if not)
3. Friend receives the anchor request in their pending requests
4. Friend can accept or reject the request
5. If accepted, the friend can now view that roadmap when visiting the user's profile
6. The roadmap owner can remove the anchor at any time

---

## Messaging

The messaging system provides real-time communication between friends through thread-based conversations. When a friend request is accepted, a private thread is automatically created between the two users.

### Features
- **Auto-created threads**: Threads are automatically created when friend requests are accepted
- **Real-time messaging**: WebSocket-based messaging for instant communication
- **Message persistence**: All messages are stored in the database
- **Thread isolation**: Users can only access threads they're participants in
- **Message history**: Retrieve past messages for any thread

### GET /api/v1/threads
Get all threads for the authenticated user with participant information.

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/threads"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "thread_id": "thread-uuid",
      "name": "",
      "participants": [
        {
          "user_id": "user-uuid-1",
          "first_name": "John",
          "last_name": "Doe",
          "email": "john@example.com",
          "phone_number": "+1-555-123-4567",
          "CreatedAt": "2024-01-01T09:00:00Z"
        },
        {
          "user_id": "user-uuid-2",
          "first_name": "Jane",
          "last_name": "Smith",
          "email": "jane@example.com",
          "phone_number": "+1-555-987-6543",
          "CreatedAt": "2024-01-01T10:00:00Z"
        }
      ],
      "created_at": "2024-01-01T10:30:00Z",
      "updated_at": "2024-01-01T10:30:00Z"
    }
  ]
}
```

### GET /api/v1/threads/:threadid/messages
Get message history for a specific thread.

**URL Parameters:**
- `threadid` (UUID) - Thread ID

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/threads/thread-uuid/messages"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "message_id": "message-uuid-1",
      "thread_id": "thread-uuid",
      "user_id": "user-uuid-1",
      "content": "Hello!",
      "created_at": "2024-01-01T10:31:00Z",
      "user": {
        "user_id": "user-uuid-1",
        "first_name": "John",
        "last_name": "Doe",
        "email": "john@example.com",
        "phone_number": "+1-555-123-4567",
        "CreatedAt": "2024-01-01T09:00:00Z"
      }
    },
    {
      "message_id": "message-uuid-2",
      "thread_id": "thread-uuid",
      "user_id": "user-uuid-2",
      "content": "Hi there!",
      "created_at": "2024-01-01T10:32:00Z",
      "user": {
        "user_id": "user-uuid-2",
        "first_name": "Jane",
        "last_name": "Smith",
        "email": "jane@example.com",
        "phone_number": "+1-555-987-6543",
        "CreatedAt": "2024-01-01T10:00:00Z"
      }
    }
  ]
}
```

**Error Responses:**
- `403 Forbidden` - You are not a participant of this thread
- `404 Not Found` - Thread not found

### WebSocket: GET /api/v1/ws/messages/:threadid
WebSocket endpoint for real-time messaging within a specific thread.

**URL Parameters:**
- `threadid` (UUID) - Thread ID

**Connection:**
```javascript
// Example WebSocket connection
const ws = new WebSocket('ws://localhost:8080/api/v1/ws/messages/thread-uuid', {
  headers: {
    'Authorization': 'Bearer <token>'
  }
});
```

**Authentication:**
- Include Bearer token in the request header when establishing WebSocket connection
- Only thread participants can connect

**Sending Messages:**
Send JSON messages through the WebSocket connection:
```json
{
  "content": "Hello from WebSocket!"
}
```

**Receiving Messages:**
All participants in the thread receive messages in this format:
```json
{
  "message_id": "message-uuid",
  "thread_id": "thread-uuid",
  "user_id": "sender-user-uuid",
  "content": "Hello from WebSocket!",
  "created_at": "2024-01-01T10:35:00.123Z"
}
```

**Message Flow:**
1. Client sends JSON message with `content` field
2. Server validates and persists message to database
3. Server broadcasts the saved message (with `message_id` and `created_at`) to all connected clients in the thread
4. All participants receive the message in real-time

**Error Messages:**
Errors are sent back to the client as JSON:
```json
{
  "error": "invalid message format"
}
```

**Error Scenarios:**
- Empty message content: `{"error": "message content cannot be empty"}`
- Invalid JSON format: `{"error": "invalid message format"}`
- Database save failure: `{"error": "failed to save message"}`

**Connection Notes:**
- WebSocket connections are automatically upgraded from HTTP
- Connection verification checks that user is a thread participant
- Each thread has its own broadcast room
- Messages are only sent to clients connected to the same thread
- Graceful disconnection handling

---

## Roadmap Management

### POST /api/v1/roadmaps
Create a new roadmap.

**Request Body:**
```json
{
  "goal": "Learn Web Development",
  "description": "Complete roadmap to become a full-stack web developer",
  "days": 90,
  "start_date": "2024-01-01T00:00:00Z"
}
```

**Response (201 Created):**
```json
{
  "data": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "goal": "Learn Web Development",
    "description": "Complete roadmap to become a full-stack web developer",
    "days": 90,
    "completed": false,
    "start_date": "2024-01-01T00:00:00Z",
    "milestones": []
  }
}
```

### GET /api/v1/roadmaps
Get all roadmaps for the authenticated user.

**Response (200 OK):**
```json
{
  "data": [
    {
      "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "goal": "Learn Web Development",
      "description": "Complete roadmap to become a full-stack web developer",
      "days": 90,
      "completed": false,
      "start_date": "2024-01-01T00:00:00Z",
      "milestones": []
    }
  ]
}
```

### GET /api/v1/roadmaps/:roadmapid
Get a specific roadmap by ID.

**URL Parameters:**
- `roadmapid` (UUID) - Roadmap ID

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "goal": "Learn Web Development",
    "description": "Complete roadmap to become a full-stack web developer",
    "days": 90,
    "completed": false,
    "start_date": "2024-01-01T00:00:00Z",
    "milestones": []
  }
}
```

### PATCH /api/v1/roadmaps/:roadmapid
Update a roadmap.

**URL Parameters:**
- `roadmapid` (UUID) - Roadmap ID

**Request Body:**
```json
{
  "goal": "Advanced Web Development",
  "description": "Updated roadmap description",
  "completed": true
}
```

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "goal": "Advanced Web Development",
    "description": "Updated roadmap description",
    "days": 90,
    "completed": true,
    "start_date": "2024-01-01T00:00:00Z",
    "milestones": []
  }
}
```

### DELETE /api/v1/roadmaps/:roadmapid
Delete a roadmap.

**URL Parameters:**
- `roadmapid` (UUID) - Roadmap ID

**Response (200 OK):**
```json
{
  "message": "Roadmap deleted successfully"
}
```

### GET /api/v1/roadmaps/:roadmapid/milestones
Get all milestones for a specific roadmap.

**URL Parameters:**
- `roadmapid` (UUID) - Roadmap ID

**Response (200 OK):**
```json
{
  "data": [
    {
      "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
      "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "sub_goal": "HTML & CSS Fundamentals",
      "description": "Learn the basics of HTML and CSS",
      "day": 30,
      "reached": false,
      "tasks": []
    }
  ]
}
```

---

## Milestone Management

### GET /api/v1/milestones/:milestoneid
Get a specific milestone by ID.

**URL Parameters:**
- `milestoneid` (UUID) - Milestone ID

**Response (200 OK):**
```json
{
  "data": {
    "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "sub_goal": "HTML & CSS Fundamentals",
    "description": "Learn the basics of HTML and CSS",
    "day": 30,
    "reached": false,
    "tasks": []
  }
}
```

### PATCH /api/v1/milestones/:milestoneid
Update a milestone.

**URL Parameters:**
- `milestoneid` (UUID) - Milestone ID

**Request Body:**
```json
{
  "sub_goal": "Advanced HTML & CSS",
  "description": "Updated milestone description",
  "reached": true
}
```

**Response (200 OK):**
```json
{
  "data": {
    "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "sub_goal": "Advanced HTML & CSS",
    "description": "Updated milestone description",
    "day": 30,
    "reached": true,
    "tasks": []
  }
}
```

### DELETE /api/v1/milestones/:milestoneid
Delete a milestone.

**URL Parameters:**
- `milestoneid` (UUID) - Milestone ID

**Response (200 OK):**
```json
{
  "message": "Milestone deleted successfully"
}
```

### GET /api/v1/milestones/:milestoneid/tasks
Get all tasks for a specific milestone.

**URL Parameters:**
- `milestoneid` (UUID) - Milestone ID

**Response (200 OK):**
```json
{
  "data": [
    {
      "task_id": "550e8400-e29b-41d4-a716-446655440003",
      "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
      "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "title": "Learn HTML Tags",
      "description": "Study common HTML tags and their usage",
      "day": 5,
      "completed": false,
      "steps": []
    }
  ]
}
```

---

## Task Management

### POST /api/v1/tasks
Create a new task.

**Request Body:**
```json
{
  "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
  "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
  "title": "Learn HTML Tags",
  "description": "Study common HTML tags and their usage",
  "day": 5
}
```

**Note:** `roadmap_id` is optional - it will be automatically derived from the milestone if not provided.

**Response (201 Created):**
```json
{
  "data": {
    "task_id": "550e8400-e29b-41d4-a716-446655440003",
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Learn HTML Tags",
    "description": "Study common HTML tags and their usage",
    "day": 5,
    "completed": false,
    "steps": []
  }
}
```

### GET /api/v1/tasks/user
Get all tasks for the current authenticated user with optional filters.

**Query Parameters:**
- `start_date` (optional) - Filter tasks by start date (format: YYYY-MM-DD)
- `end_date` (optional) - Filter tasks by end date (format: YYYY-MM-DD)
- `roadmap_id` (optional) - Filter tasks by roadmap ID (UUID)

**Examples:**
- Get all tasks: `GET /api/v1/tasks/user`
- Get tasks for a specific date: `GET /api/v1/tasks/user?start_date=2024-01-01`
- Get tasks in a date range: `GET /api/v1/tasks/user?start_date=2024-01-01&end_date=2024-01-31`
- Get tasks for a roadmap: `GET /api/v1/tasks/user?roadmap_id=550e8400-e29b-41d4-a716-446655440001`
- Combine filters: `GET /api/v1/tasks/user?roadmap_id=550e8400-e29b-41d4-a716-446655440001&start_date=2024-01-01`

**Response (200 OK):**
```json
{
  "data": [
    {
      "task_id": "550e8400-e29b-41d4-a716-446655440003",
      "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
      "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "title": "Learn HTML Tags",
      "description": "Study common HTML tags and their usage",
      "day": 5,
      "completed": false,
      "steps": []
    }
  ]
}
```

### GET /api/v1/tasks/:taskid
Get a specific task by ID.

**URL Parameters:**
- `taskid` (UUID) - Task ID

**Response (200 OK):**
```json
{
  "data": {
    "task_id": "550e8400-e29b-41d4-a716-446655440003",
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Learn HTML Tags",
    "description": "Study common HTML tags and their usage",
    "day": 5,
    "completed": false,
    "steps": []
  }
}
```

### PATCH /api/v1/tasks/:taskid
Update a task.

**URL Parameters:**
- `taskid` (UUID) - Task ID

**Request Body:**
```json
{
  "title": "Master HTML Tags",
  "description": "Complete understanding of HTML tags",
  "completed": true
}
```

**Response (200 OK):**
```json
{
  "data": {
    "task_id": "550e8400-e29b-41d4-a716-446655440003",
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "milestone_id": "550e8400-e29b-41d4-a716-446655440002",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Master HTML Tags",
    "description": "Complete understanding of HTML tags",
    "day": 5,
    "completed": true,
    "steps": []
  }
}
```

### DELETE /api/v1/tasks/:taskid
Delete a task.

**URL Parameters:**
- `taskid` (UUID) - Task ID

**Response (200 OK):**
```json
{
  "message": "Task deleted successfully"
}
```

---

## Habits Management

The habits system provides comprehensive tracking for recurring habits and one-time todos with flexible scheduling, completion tracking, statistics (streaks and completion rates), and calendar views.

### Features
- **One-time todos**: Tasks with a specific due date
- **Recurring habits**: Daily, weekly (with day-of-week selection), or monthly patterns
- **Completion tracking**: Mark habits complete for specific dates with optional notes
- **Statistics**: Track current streaks, 7-day and 30-day completion rates
- **Calendar views**: Query habits for specific dates or date ranges
- **Archiving**: Soft-archive habits without deleting history

### Recurrence Types
- **`none`**: One-time todo with a `due_date`
- **`daily`**: Repeats every day
- **`weekly`**: Repeats on specific days of the week (uses bitmask)
- **`monthly`**: Repeats on a specific day of the month

### Days of Week Bitmask
For weekly recurring habits, use a bitmask to represent which days the habit repeats:
- `1` = Sunday, `2` = Monday, `4` = Tuesday, `8` = Wednesday
- `16` = Thursday, `32` = Friday, `64` = Saturday
- **Common values**: `62` (weekdays), `65` (weekends), `127` (every day)

### POST /api/v1/habits
Create a new habit or todo.

**Request Body (Daily Habit):**
```json
{
  "title": "Morning meditation",
  "description": "10 minutes of mindfulness",
  "recurrence_type": "daily",
  "start_date": "2024-01-01T00:00:00Z"
}
```

**Request Body (Weekly Habit - Weekdays):**
```json
{
  "title": "Gym workout",
  "recurrence_type": "weekly",
  "days_of_week": 62,
  "start_date": "2024-01-01T00:00:00Z"
}
```

**Request Body (Monthly Habit):**
```json
{
  "title": "Monthly review",
  "recurrence_type": "monthly",
  "day_of_month": 1,
  "start_date": "2024-01-01T00:00:00Z"
}
```

**Request Body (One-time Todo):**
```json
{
  "title": "Submit tax returns",
  "recurrence_type": "none",
  "due_date": "2024-04-15T00:00:00Z",
  "start_date": "2024-01-01T00:00:00Z"
}
```

**Response (201 Created):**
```json
{
  "data": {
    "habit_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Morning meditation",
    "description": "10 minutes of mindfulness",
    "recurrence_type": "daily",
    "days_of_week": 0,
    "day_of_month": null,
    "due_date": null,
    "start_date": "2024-01-01T00:00:00Z",
    "end_date": null,
    "archived": false,
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:00:00Z"
  }
}
```

**Validation Rules:**
- Weekly recurrence requires `days_of_week` (1-127)
- Monthly recurrence requires `day_of_month` (1-31)
- One-time todos require `due_date`

### GET /api/v1/habits
Get all habits for the current user.

**Query Parameters:**
- `include_archived` (optional, boolean) - Include archived habits (default: false)

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits?include_archived=false"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "habit_id": "550e8400-e29b-41d4-a716-446655440001",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "title": "Morning meditation",
      "description": "10 minutes of mindfulness",
      "recurrence_type": "daily",
      "days_of_week": 0,
      "day_of_month": null,
      "due_date": null,
      "start_date": "2024-01-01T00:00:00Z",
      "end_date": null,
      "archived": false,
      "created_at": "2024-01-01T10:00:00Z",
      "updated_at": "2024-01-01T10:00:00Z"
    }
  ]
}
```

### GET /api/v1/habits/today
Get today's habits with completion status.

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/today"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "habit_id": "550e8400-e29b-41d4-a716-446655440001",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "title": "Morning meditation",
      "description": "10 minutes of mindfulness",
      "recurrence_type": "daily",
      "days_of_week": 0,
      "day_of_month": null,
      "due_date": null,
      "start_date": "2024-01-01T00:00:00Z",
      "end_date": null,
      "archived": false,
      "created_at": "2024-01-01T10:00:00Z",
      "updated_at": "2024-01-01T10:00:00Z",
      "is_completed": true,
      "completed_at": "2024-01-15T08:30:00Z"
    }
  ]
}
```

### GET /api/v1/habits/date/:date
Get habits for a specific date with completion status.

**URL Parameters:**
- `date` (required) - Date in YYYY-MM-DD format

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/date/2024-01-15"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "habit_id": "550e8400-e29b-41d4-a716-446655440001",
      "title": "Morning meditation",
      "recurrence_type": "daily",
      "is_completed": false,
      "completed_at": null
    }
  ]
}
```

**Error Responses:**
- `400 Bad Request` - Invalid date format

### GET /api/v1/habits/range
Get habits for a date range (calendar view).

**Query Parameters:**
- `start` (required) - Start date in YYYY-MM-DD format
- `end` (required) - End date in YYYY-MM-DD format (max 90 days from start)

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/range?start=2024-01-01&end=2024-01-31"
```

**Response (200 OK):**
```json
{
  "data": {
    "2024-01-01": [
      {
        "habit_id": "550e8400-e29b-41d4-a716-446655440001",
        "title": "Morning meditation",
        "recurrence_type": "daily",
        "is_completed": true,
        "completed_at": "2024-01-01T08:00:00Z"
      }
    ],
    "2024-01-02": [
      {
        "habit_id": "550e8400-e29b-41d4-a716-446655440001",
        "title": "Morning meditation",
        "recurrence_type": "daily",
        "is_completed": false,
        "completed_at": null
      }
    ]
  }
}
```

**Error Responses:**
- `400 Bad Request` - Invalid date format or range exceeds 90 days

### GET /api/v1/habits/:habitid
Get a specific habit by ID.

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Response (200 OK):**
```json
{
  "data": {
    "habit_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Morning meditation",
    "description": "10 minutes of mindfulness",
    "recurrence_type": "daily",
    "days_of_week": 0,
    "day_of_month": null,
    "due_date": null,
    "start_date": "2024-01-01T00:00:00Z",
    "end_date": null,
    "archived": false,
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:00:00Z"
  }
}
```

**Error Responses:**
- `404 Not Found` - Habit not found

### PATCH /api/v1/habits/:habitid
Update a habit (all fields optional).

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Request Body:**
```json
{
  "title": "Evening meditation",
  "description": "15 minutes of mindfulness",
  "end_date": "2024-12-31T23:59:59Z"
}
```

**Response (200 OK):**
```json
{
  "data": {
    "habit_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "title": "Evening meditation",
    "description": "15 minutes of mindfulness",
    "recurrence_type": "daily",
    "days_of_week": 0,
    "day_of_month": null,
    "due_date": null,
    "start_date": "2024-01-01T00:00:00Z",
    "end_date": "2024-12-31T23:59:59Z",
    "archived": false,
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-15T14:30:00Z"
  }
}
```

**Error Responses:**
- `404 Not Found` - Habit not found

### DELETE /api/v1/habits/:habitid
Delete a habit permanently (cascades to completions).

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Response (200 OK):**
```json
{
  "message": "Habit deleted successfully"
}
```

**Error Responses:**
- `404 Not Found` - Habit not found

### POST /api/v1/habits/:habitid/archive
Archive a habit (soft delete, preserves history).

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Example Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/550e8400-e29b-41d4-a716-446655440001/archive"
```

**Response (200 OK):**
```json
{
  "message": "Habit archived successfully"
}
```

**Error Responses:**
- `404 Not Found` - Habit not found

### POST /api/v1/habits/:habitid/complete
Mark a habit complete for a specific date.

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Request Body (Complete for Today):**
```json
{
  "notes": "Felt great today!"
}
```

**Request Body (Complete for Specific Date):**
```json
{
  "date": "2024-01-15T00:00:00Z",
  "notes": "Made up for yesterday"
}
```

**Response (201 Created):**
```json
{
  "data": {
    "completion_id": "550e8400-e29b-41d4-a716-446655440010",
    "habit_id": "550e8400-e29b-41d4-a716-446655440001",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "completed_at": "2024-01-15T08:30:00Z",
    "date": "2024-01-15",
    "notes": "Felt great today!",
    "created_at": "2024-01-15T08:30:00Z"
  }
}
```

**Error Responses:**
- `400 Bad Request` - Habit already completed for this date
- `404 Not Found` - Habit not found

### DELETE /api/v1/habits/:habitid/complete
Remove a completion (uncomplete a habit).

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Query Parameters:**
- `date` (optional) - Date to remove completion for (YYYY-MM-DD format, defaults to today)

**Example Request:**
```bash
curl -X DELETE \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/550e8400-e29b-41d4-a716-446655440001/complete?date=2024-01-15"
```

**Response (200 OK):**
```json
{
  "message": "Completion removed successfully"
}
```

### GET /api/v1/habits/:habitid/completions
Get completion history for a habit.

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Query Parameters:**
- `limit` (optional, integer) - Maximum number of completions to return (default: 30)

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/550e8400-e29b-41d4-a716-446655440001/completions?limit=30"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "completion_id": "550e8400-e29b-41d4-a716-446655440010",
      "habit_id": "550e8400-e29b-41d4-a716-446655440001",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "completed_at": "2024-01-15T08:30:00Z",
      "date": "2024-01-15",
      "notes": "Felt great today!",
      "created_at": "2024-01-15T08:30:00Z"
    },
    {
      "completion_id": "550e8400-e29b-41d4-a716-446655440011",
      "habit_id": "550e8400-e29b-41d4-a716-446655440001",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "completed_at": "2024-01-14T09:00:00Z",
      "date": "2024-01-14",
      "notes": null,
      "created_at": "2024-01-14T09:00:00Z"
    }
  ]
}
```

### GET /api/v1/habits/:habitid/stats
Get statistics for a habit including streaks and completion rates.

**URL Parameters:**
- `habitid` (UUID) - Habit ID

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/habits/550e8400-e29b-41d4-a716-446655440001/stats"
```

**Response (200 OK):**
```json
{
  "data": {
    "current_streak": 7,
    "completion_rate_7d": 0.857,
    "completion_rate_30d": 0.733,
    "total_completions": 42
  }
}
```

**Statistics Explained:**
- **current_streak**: Number of consecutive days the habit has been completed
- **completion_rate_7d**: Completion rate over the last 7 days (0.0 to 1.0)
- **completion_rate_30d**: Completion rate over the last 30 days (0.0 to 1.0)
- **total_completions**: Total number of times the habit has been completed

**Error Responses:**
- `404 Not Found` - Habit not found

---

## AI Agent Endpoints

### POST /api/v1/agent/generate
Generate a roadmap using AI based on a user prompt.

**Request Body:**
```json
{
  "user_prompt": "I want to learn machine learning and become proficient in 6 months"
}
```

**Response (200 OK):**
```json
{
  "data": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440001",
    "goal": "Learn Machine Learning",
    "description": "6-month comprehensive machine learning roadmap",
    "days": 180,
    "milestones": [
      {
        "sub_goal": "Python Fundamentals",
        "description": "Learn Python programming basics",
        "day": 30,
        "tasks": []
      }
    ]
  }
}
```

### POST /api/v1/agent/generate-async
Generate a roadmap asynchronously with real-time progress updates. Recommended for production use as generation can take 2-3 minutes.

**Request Body:**
```json
{
  "goalName": "Learn Web Development",
  "goalDescription": "I want to become a full-stack web developer in 90 days",
  "timeframe": "90 days",
  "hoursPerDay": 2
}
```

**Response (202 Accepted):**
```json
{
  "status": "pending",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "roadmap_id": "550e8400-e29b-41d4-a716-446655440011",
  "message": "Your roadmap is being processed",
  "ws_url": "ws/agent/550e8400-e29b-41d4-a716-446655440010"
}
```

**How it works:**
1. Server creates a background job and draft roadmap immediately
2. Returns job_id and roadmap_id to client
3. Client can:
   - Connect via WebSocket at `/ws/agent/:jobid` for real-time updates (recommended)
   - Poll GET `/jobs/:jobid/status` every 2-3 seconds
   - Receive push notification when complete (if token registered)
4. When complete, roadmap is fully populated and ready to view

**Progress Updates via WebSocket:**
```json
{
  "type": "progress",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "progress": 45,
  "status": "processing",
  "message": "Generating strategic roadmap..."
}
```

**Completion Message:**
```json
{
  "type": "complete",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "progress": 100,
  "status": "completed",
  "message": "Your roadmap is ready for review!",
  "data": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440011"
  }
}
```

### POST /api/v1/agent/sessions
Start a new AI session for interactive roadmap generation.

**Response (200 OK):**
```json
{
  "data": {
    "session_id": "550e8400-e29b-41d4-a716-446655440004",
    "message": "What kind of goal would you like to achieve?"
  }
}
```

### GET /api/v1/agent/sessions/:id
Get an AI session by ID.

**URL Parameters:**
- `id` (UUID) - Session ID

**Response (200 OK):**
```json
{
  "data": {
    "session_id": "550e8400-e29b-41d4-a716-446655440004",
    "user_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "collecting",
    "context": [
      {
        "role": "assistant",
        "content": "What kind of goal would you like to achieve?"
      }
    ],
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:00:00Z"
  }
}
```

### POST /api/v1/agent/sessions/:id/reply
Send a reply to an AI session.

**URL Parameters:**
- `id` (UUID) - Session ID

**Request Body:**
```json
{
  "message": "I want to learn web development"
}
```

**Response (200 OK) - Session In Progress:**
```json
{
  "data": {
    "message": "Great! How much time do you have available per day for learning?",
    "complete": false
  }
}
```

**Response (200 OK) - Session Complete:**
```json
{
  "data": {
    "message": "Perfect! I've created your roadmap.",
    "complete": true,
    "roadmap": {
      "goal": "Learn Web Development",
      "description": "Complete web development roadmap",
      "days": 90,
      "milestones": []
    }
  }
}
```

---

## WebSocket Endpoints

### GET /api/v1/ws/messages/:threadid
WebSocket connection for real-time messaging.

**URL Parameters:**
- `threadid` (UUID) - Thread ID

**Connection:** Upgrade to WebSocket protocol

**Message Format:**
- Supports text messages
- Echo functionality (messages are echoed back)
- Connection includes read/write deadlines and ping/pong handling

### GET /api/v1/ws/agent/:jobid
WebSocket connection for real-time AI roadmap generation progress updates.

**URL Parameters:**
- `jobid` (UUID) - Background job ID

**Connection:** Upgrade to WebSocket protocol

**Message Types:**
Client receives JSON messages with one of these types:

**Status Update (sent immediately on connect):**
```json
{
  "type": "status",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "progress": 0,
  "status": "pending",
  "message": "Initializing..."
}
```

**Progress Update:**
```json
{
  "type": "progress",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "progress": 45,
  "status": "processing",
  "message": "Generating strategic roadmap..."
}
```

**Completion:**
```json
{
  "type": "complete",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "progress": 100,
  "status": "completed",
  "message": "Your roadmap is ready for review!",
  "data": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440011"
  }
}
```

**Error:**
```json
{
  "type": "error",
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "status": "failed",
  "message": "Failed to generate roadmap",
  "data": {
    "error": "OpenAI API error: rate limit exceeded"
  }
}
```

**Ping/Pong:**
Client can send `{"type": "ping"}` to keep connection alive.
Server responds with `{"type": "pong"}`.

---

## Background Jobs

### GET /api/v1/jobs/:jobid/status
Get the status of a background job. Use WebSocket for real-time updates instead of polling.

**URL Parameters:**
- `jobid` (UUID) - Job ID

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/jobs/550e8400-e29b-41d4-a716-446655440010/status"
```

**Response (200 OK) - Job in Progress:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "status": "processing",
  "progress": 45,
  "created_at": "2024-01-01T10:00:00Z"
}
```

**Response (200 OK) - Job Completed:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "status": "completed",
  "progress": 100,
  "created_at": "2024-01-01T10:00:00Z",
  "result": {
    "roadmap_id": "550e8400-e29b-41d4-a716-446655440011",
    "macromap_id": "550e8400-e29b-41d4-a716-446655440012"
  },
  "completed_at": "2024-01-01T10:03:15Z"
}
```

**Response (200 OK) - Job Failed:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440010",
  "status": "failed",
  "progress": 35,
  "created_at": "2024-01-01T10:00:00Z",
  "error": "OpenAI API error: rate limit exceeded",
  "failed_at": "2024-01-01T10:01:45Z"
}
```

**Error Responses:**
- `404 Not Found` - Job not found or user not authorized

---

## Push Notifications

### POST /api/v1/push/tokens
Register or update an Expo push notification token for the current user.

**Request Body:**
```json
{
  "expo_token": "ExponentPushToken[xxxxxxxxxxxxxxxxxxxxxx]",
  "platform": "ios",
  "device_id": "iPhone14,2"
}
```

**Example Request:**
```bash
curl -X POST \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"expo_token": "ExponentPushToken[xxx]", "platform": "ios"}' \
  "http://localhost:8080/api/v1/push/tokens"
```

**Response (200 OK):**
```json
{
  "message": "Push token registered successfully",
  "token_id": "550e8400-e29b-41d4-a716-446655440020"
}
```

**Error Responses:**
- `400 Bad Request` - Invalid request body or platform

### DELETE /api/v1/push/tokens/:token
Deactivate a push notification token (e.g., on logout or app uninstall).

**URL Parameters:**
- `token` (string) - Expo push token to deactivate

**Example Request:**
```bash
curl -X DELETE \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/push/tokens/ExponentPushToken[xxx]"
```

**Response (200 OK):**
```json
{
  "message": "Push token deactivated successfully"
}
```

---

## Notifications

### GET /api/v1/notifications
Get all unread notifications for the current user.

**Example Request:**
```bash
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/notifications"
```

**Response (200 OK):**
```json
{
  "data": [
    {
      "notification_id": "550e8400-e29b-41d4-a716-446655440030",
      "user_id": "550e8400-e29b-41d4-a716-446655440000",
      "type": "roadmap_complete",
      "title": "Your roadmap is ready for review!",
      "body": "Your Learn Web Development roadmap has been generated successfully.",
      "data": {
        "roadmap_id": "550e8400-e29b-41d4-a716-446655440011"
      },
      "status": "sent",
      "sent_at": "2024-01-01T10:03:20Z",
      "read_at": null,
      "created_at": "2024-01-01T10:03:15Z"
    }
  ]
}
```

### PUT /api/v1/notifications/:alertid/read
Mark a notification as read.

**URL Parameters:**
- `alertid` (UUID) - Notification ID

**Example Request:**
```bash
curl -X PUT \
  -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/v1/notifications/550e8400-e29b-41d4-a716-446655440030/read"
```

**Response (200 OK):**
```json
{
  "message": "Notification marked as read"
}
```

---

## Data Models

### User
```json
{
  "user_id": "string (UUID)",
  "first_name": "string",
  "last_name": "string",
  "email": "string",
  "phone_number": "string",
  "CreatedAt": "datetime"
}
```

### Friendship
```json
{
  "id": "string (UUID)",
  "requester_id": "string (UUID)",
  "addressee_id": "string (UUID)",
  "status": "string (pending|accepted|rejected|blocked)",
  "restrictions": {
    "allowed_roadmap_ids": ["string (UUID)"]
  },
  "created_at": "datetime",
  "updated_at": "datetime (nullable)",
  "requester": "User object (when preloaded)",
  "addressee": "User object (when preloaded)"
}
```

### UserSearchResult
```json
{
  "user_id": "string (UUID)",
  "first_name": "string",
  "last_name": "string",
  "email": "string",
  "friendship_status": "string (none|pending|accepted)"
}
```

### FriendResponse
```json
{
  "friendship_id": "string (UUID)",
  "friend": "User object",
  "restrictions": {
    "allowed_roadmap_ids": ["string (UUID)"]
  },
  "created_at": "datetime"
}
```

### FriendProfileResponse
```json
{
  "user": "User object",
  "allowed_roadmaps": ["array of Roadmap objects"]
}
```

### AnchorRequest
```json
{
  "request_id": "string (UUID)",
  "roadmap_id": "string (UUID)",
  "requester_id": "string (UUID) - User who owns the roadmap",
  "anchor_id": "string (UUID) - User being asked to be the anchor",
  "status": "string (pending|accepted|rejected)",
  "created_at": "datetime",
  "updated_at": "datetime (nullable)",
  "roadmap": "Roadmap object (when preloaded)",
  "requester": "User object (when preloaded)",
  "anchor": "User object (when preloaded)"
}
```

### Roadmap
```json
{
  "roadmap_id": "string (UUID)",
  "user_id": "string (UUID)",
  "goal": "string",
  "description": "string",
  "days": "integer",
  "completed": "boolean",
  "start_date": "datetime",
  "anchor_id": "string (UUID, nullable) - User ID of accountability partner",
  "milestones": "array of Milestone objects"
}
```

### Milestone
```json
{
  "milestone_id": "string (UUID)",
  "roadmap_id": "string (UUID)",
  "user_id": "string (UUID)",
  "sub_goal": "string",
  "description": "string",
  "day": "integer",
  "reached": "boolean",
  "tasks": "array of Task objects"
}
```

### Task
```json
{
  "task_id": "string (UUID)",
  "roadmap_id": "string (UUID)",
  "milestone_id": "string (UUID)",
  "user_id": "string (UUID)",
  "title": "string",
  "description": "string",
  "day": "integer",
  "completed": "boolean",
  "steps": "array of Step objects"
}
```

### Step
```json
{
  "step_id": "string (UUID)",
  "task_id": "string (UUID)",
  "description": "string",
  "completed": "boolean",
  "number": "integer"
}
```

### Thread
```json
{
  "thread_id": "string (UUID)",
  "name": "string",
  "created_at": "datetime",
  "updated_at": "datetime"
}
```

### ThreadWithParticipants
```json
{
  "thread_id": "string (UUID)",
  "name": "string",
  "participants": "array of User objects",
  "created_at": "datetime",
  "updated_at": "datetime"
}
```

### Message
```json
{
  "message_id": "string (UUID)",
  "thread_id": "string (UUID)",
  "user_id": "string (UUID)",
  "content": "string",
  "created_at": "datetime",
  "updated_at": "datetime",
  "deleted_at": "datetime (nullable)"
}
```

### MessageResponse
```json
{
  "message_id": "string (UUID)",
  "thread_id": "string (UUID)",
  "user_id": "string (UUID)",
  "content": "string",
  "created_at": "datetime",
  "user": "User object (nullable)"
}
```

### Habit
```json
{
  "habit_id": "string (UUID)",
  "user_id": "string (UUID)",
  "title": "string",
  "description": "string (nullable)",
  "recurrence_type": "string (none|daily|weekly|monthly)",
  "days_of_week": "integer (0-127) - Bitmask for weekly recurrence",
  "day_of_month": "integer (1-31, nullable) - Day for monthly recurrence",
  "due_date": "datetime (nullable) - For one-time todos",
  "start_date": "datetime - When habit becomes active",
  "end_date": "datetime (nullable) - When habit ends",
  "archived": "boolean",
  "created_at": "datetime",
  "updated_at": "datetime"
}
```

### HabitCompletion
```json
{
  "completion_id": "string (UUID)",
  "habit_id": "string (UUID)",
  "user_id": "string (UUID)",
  "completed_at": "datetime - When marked complete",
  "date": "date - Calendar date this completion counts for",
  "notes": "string (nullable) - Optional notes",
  "created_at": "datetime"
}
```

### HabitWithStatus
```json
{
  "habit_id": "string (UUID)",
  "user_id": "string (UUID)",
  "title": "string",
  "description": "string (nullable)",
  "recurrence_type": "string (none|daily|weekly|monthly)",
  "days_of_week": "integer (0-127)",
  "day_of_month": "integer (1-31, nullable)",
  "due_date": "datetime (nullable)",
  "start_date": "datetime",
  "end_date": "datetime (nullable)",
  "archived": "boolean",
  "created_at": "datetime",
  "updated_at": "datetime",
  "is_completed": "boolean - Whether completed for queried date",
  "completed_at": "datetime (nullable) - When completed (null if not)"
}
```

### HabitStats
```json
{
  "current_streak": "integer - Consecutive days completed",
  "completion_rate_7d": "float (0.0-1.0) - 7-day completion rate",
  "completion_rate_30d": "float (0.0-1.0) - 30-day completion rate",
  "total_completions": "integer - Total times completed"
}
```

---

## Error Responses

### Common HTTP Status Codes
- `200 OK` - Request successful
- `201 Created` - Resource created successfully
- `400 Bad Request` - Invalid request data
- `401 Unauthorized` - Authentication required or invalid
- `404 Not Found` - Resource not found
- `500 Internal Server Error` - Server error

### Error Response Format
```json
{
  "error": "Error message describing what went wrong"
}
```

### Success Response Format
```json
{
  "data": "Response data object"
}
```

### Message Response Format
```json
{
  "message": "Success message"
}
```

### BackgroundJob
```json
{
  "job_id": "string (UUID)",
  "user_id": "string (UUID)",
  "type": "string (roadmap_generation)",
  "status": "string (pending|processing|completed|failed)",
  "progress": "integer (0-100)",
  "payload": "object - Input data (GoalRequest for roadmap generation)",
  "result": {
    "roadmap_id": "string (UUID)",
    "macromap_id": "string (UUID)"
  },
  "error": "string (nullable) - Error message if failed",
  "started_at": "datetime (nullable)",
  "completed_at": "datetime (nullable)",
  "created_at": "datetime",
  "updated_at": "datetime"
}
```

### ProgressUpdate
```json
{
  "type": "string (progress|status|complete|error)",
  "job_id": "string (UUID)",
  "progress": "integer (0-100)",
  "status": "string (pending|processing|completed|failed)",
  "message": "string - Human-readable status message",
  "data": "object (nullable) - Additional context (roadmap_id on complete, error on failure)"
}
```

### PushToken
```json
{
  "token_id": "string (UUID)",
  "user_id": "string (UUID)",
  "expo_token": "string - Expo push notification token",
  "platform": "string (ios|android)",
  "device_id": "string (nullable)",
  "is_active": "boolean",
  "last_used_at": "datetime",
  "created_at": "datetime"
}
```

### Notification
```json
{
  "notification_id": "string (UUID)",
  "user_id": "string (UUID)",
  "type": "string (roadmap_complete|roadmap_failed)",
  "title": "string",
  "body": "string",
  "data": {
    "roadmap_id": "string (UUID)",
    "error": "string (for failed notifications)"
  },
  "status": "string (pending|sent|failed|read)",
  "sent_at": "datetime (nullable)",
  "read_at": "datetime (nullable)",
  "error": "string (nullable) - Error if notification failed to send",
  "created_at": "datetime"
}
```

---

## Notes

1. All UUID parameters are validated using middleware
2. All endpoints under `/api/v1/` require authentication
3. The server uses Gin framework with JSON responses
4. Database operations use GORM with PostgreSQL
5. WebSocket connections support real-time messaging
6. AI agent functionality requires external AI service integration
7. Friend management features:
   - Friendships use UUID-based IDs for consistency
   - RLS (Row Level Security) policies control data access
   - Friends can only view roadmaps where they are the accountability partner (anchor)
   - Only the addressee can accept/reject friend requests
   - Either party can remove a friendship
8. Anchor (Accountability Partner) features:
   - Users can request friends to be accountability partners for specific roadmaps
   - Friendship is verified before sending anchor requests (returns 403 if not friends)
   - Only the requested anchor can accept/reject anchor requests
   - Friends can only view roadmaps where they are the anchor (provides privacy)
   - Roadmap owners can remove anchors at any time
   - Anchor requests support three states: pending, accepted, rejected
9. Messaging system features:
   - Threads are automatically created when friend requests are accepted
   - WebSocket connections are thread-specific with isolated broadcast rooms
   - All messages are persisted to the database before broadcasting
   - Only thread participants can access messages and connect to WebSockets
   - Each message gets a UUID and timestamp from the server