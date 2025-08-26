# Milestone API Specification

## Base URL
All endpoints require authentication and use the user context for access control.

## Authentication
All endpoints require a valid user session with `user_id` in the request context.

---

## Endpoints

### 1. Get Milestones by Roadmap

**GET** `/roadmap/milestones/:roadmapid`

Retrieves all milestones for a specific roadmap belonging to the authenticated user.

#### Parameters
- **roadmapid** (path, required): UUID of the roadmap

#### Response
**200 OK**
```json
[
  {
    "milestone_id": "string (uuid)",
    "roadmap_id": "string (uuid)",
    "user_id": "string (uuid)",
    "sub_goal": "string",
    "description": "string",
    "day": "integer",
    "reached": "boolean",
    "tasks": [
      {
        "task_id": "string (uuid)",
        "milestone_id": "string (uuid)",
        "user_id": "string (uuid)",
        "title": "string",
        "description": "string",
        "day": "integer",
        "completed": "boolean",
        "steps": [
          {
            "step_id": "string (uuid)",
            "task_id": "string (uuid)",
            "description": "string",
            "completed": "boolean",
            "number": "integer"
          }
        ]
      }
    ]
  }
]
```

#### Error Responses
- **400 Bad Request**: Invalid roadmap ID format
- **401 Unauthorized**: Missing or invalid user authentication
- **404 Not Found**: Roadmap not found or not accessible to user

---

### 2. Get Single Milestone

**GET** `/roadmap/milestone/:milestoneid`

Retrieves a specific milestone with all its tasks and steps.

#### Parameters
- **milestoneid** (path, required): UUID of the milestone

#### Response
**200 OK**
```json
{
  "milestone_id": "string (uuid)",
  "roadmap_id": "string (uuid)",
  "user_id": "string (uuid)",
  "sub_goal": "string",
  "description": "string",
  "day": "integer",
  "reached": "boolean",
  "tasks": [
    {
      "task_id": "string (uuid)",
      "milestone_id": "string (uuid)",
      "user_id": "string (uuid)",
      "title": "string",
      "description": "string",
      "day": "integer",
      "completed": "boolean",
      "steps": [
        {
          "step_id": "string (uuid)",
          "task_id": "string (uuid)",
          "description": "string",
          "completed": "boolean",
          "number": "integer"
        }
      ]
    }
  ]
}
```

#### Error Responses
- **400 Bad Request**: Invalid milestone ID format
- **401 Unauthorized**: Missing or invalid user authentication
- **404 Not Found**: Milestone not found or not accessible to user

---

### 3. Update Milestone

**PATCH** `/roadmap/milestone/:milestoneid`

Updates a specific milestone's properties.

#### Parameters
- **milestoneid** (path, required): UUID of the milestone

#### Request Body
```json
{
  "sub_goal": "string (optional)",
  "description": "string (optional)",
  "day": "integer (optional)",
  "reached": "boolean (optional)"
}
```

#### Response
**200 OK**
```json
{
  "milestone_id": "string (uuid)",
  "roadmap_id": "string (uuid)",
  "user_id": "string (uuid)",
  "sub_goal": "string",
  "description": "string",
  "day": "integer",
  "reached": "boolean",
  "tasks": [
    {
      "task_id": "string (uuid)",
      "milestone_id": "string (uuid)",
      "user_id": "string (uuid)",
      "title": "string",
      "description": "string",
      "day": "integer",
      "completed": "boolean",
      "steps": [
        {
          "step_id": "string (uuid)",
          "task_id": "string (uuid)",
          "description": "string",
          "completed": "boolean",
          "number": "integer"
        }
      ]
    }
  ]
}
```

#### Error Responses
- **400 Bad Request**: Invalid milestone ID format or malformed request body
- **401 Unauthorized**: Missing or invalid user authentication
- **404 Not Found**: Milestone not found or not accessible to user

---

### 4. Delete Milestone

**DELETE** `/roadmap/milestone/:milestoneid`

*Note: This endpoint is currently mapped to a placeholder function (`todo`) and is not yet implemented.*

#### Parameters
- **milestoneid** (path, required): UUID of the milestone

#### Response
*To be implemented*

---

## Data Models

### Milestone Object
```json
{
  "milestone_id": "string (uuid, auto-generated)",
  "roadmap_id": "string (uuid, required)",
  "user_id": "string (uuid, auto-set from context)",
  "sub_goal": "string (required)",
  "description": "string (optional)",
  "day": "integer (required)",
  "reached": "boolean (default: false)",
  "tasks": "array of Task objects"
}
```

### Task Object
```json
{
  "task_id": "string (uuid, auto-generated)",
  "milestone_id": "string (uuid, required)",
  "user_id": "string (uuid, auto-set from context)",
  "title": "string (required)",
  "description": "string (optional)",
  "day": "integer (optional)",
  "completed": "boolean (default: false)",
  "steps": "array of Step objects"
}
```

### Step Object
```json
{
  "step_id": "string (uuid, auto-generated)",
  "task_id": "string (uuid, required)",
  "description": "string (required)",
  "completed": "boolean (default: false)",
  "number": "integer (required)"
}
```

---

## Notes

1. **Authentication**: All endpoints require user authentication. The `user_id` is automatically extracted from the request context.

2. **Access Control**: Users can only access milestones that belong to their roadmaps. The API enforces this through `user_id` filtering.

3. **Relationships**: Milestones are automatically loaded with their associated tasks and steps using GORM preloading.

4. **UUIDs**: All IDs are UUIDs and are auto-generated by the database.

5. **Delete Endpoint**: The delete milestone endpoint is currently not implemented (mapped to a `todo` function).

6. **Error Handling**: The API returns appropriate HTTP status codes. Database errors are logged server-side but may return generic error responses to the client.

7. **Validation**: The API expects valid UUID format for path parameters. Invalid UUIDs will return 400 Bad Request errors.