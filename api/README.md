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