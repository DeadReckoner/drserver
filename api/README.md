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
Get a friend's profile with roadmaps they've allowed you to see.

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
        "milestones": [...]
      }
    ]
  }
}
```

**Roadmap Visibility Rules:**
- If `restrictions.allowed_roadmap_ids` is specified, only those roadmaps are returned
- If `restrictions` is null or empty, all of the friend's roadmaps are returned

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
   - Roadmap sharing is controlled via `restrictions.allowed_roadmap_ids`
   - Only the addressee can accept/reject friend requests
   - Either party can remove a friendship
8. Messaging system features:
   - Threads are automatically created when friend requests are accepted
   - WebSocket connections are thread-specific with isolated broadcast rooms
   - All messages are persisted to the database before broadcasting
   - Only thread participants can access messages and connect to WebSockets
   - Each message gets a UUID and timestamp from the server