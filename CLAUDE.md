# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
go run main.go                              # Start server on :8080
go test ./api -run TestFunctionName -v      # Run single test
fly deploy                                  # Deploy to Fly.io
```

### Makefile

```bash
make all                                    # tidy, fmt, lint, build, test, clean
make ci                                     # tidy, fmt, build, test, clean (no lint)
make test                                   # Run tests with coverage
make lint                                   # Run golangci-lint
make fmt                                    # Format code
make build                                  # Build binary
```

**Always run `make all` after making changes to verify compilation and tests pass.**

Environment requires: `DATABASE_URL`, `API_URL`, `anon_key`, `OPENAI_API_KEY`

## Architecture

- **main.go** - Server entry point with graceful shutdown
- **api/routes/routes.go** - All API route definitions (grouped by domain)
- **api/handlers/** - Request handlers (roadmap.go, habits.go, friends.go, etc.)
- **api/utils/** - Response helpers, validation, notifications
- **database/** - GORM database operations (one file per domain)
- **types/types.go** - All domain types with GORM tags and validation
- **agent/agent.go** - OpenAI integration for AI roadmap/milestone generation
- **websocket/manager.go** - WebSocket hub for messaging and job progress
- **middleware/middleware.go** - Auth (Supabase JWT) and admin middleware
- **supabase/migrations/** - PostgreSQL migrations

## Data Model Hierarchy

```
Roadmap
├── Phases (strategic overview)
│   └── Milestones (with Spikes, Risks)
└── Milestones (tactical detail)
    └── Tasks
        └── Steps
```

## Key Patterns

### Handler Pattern
All handlers follow this pattern:
1. Get userID from context via `c.GetString("user_id")`
2. Parse/validate request body using `utils.BindJSONOrAbort(c, &req)`
3. Call database function
4. Return JSON response using `utils.DataResponse(c, statusCode, data)`

### Response Helpers (api/utils/response.go)
- `utils.DataResponse(c, statusCode, data)` - Return JSON data
- `utils.ErrorResponse(c, statusCode, message)` - Return error
- `utils.NotFoundResponse(c, "resource")` - 404 response
- `utils.BadRequestResponse(c, message)` - 400 response
- `utils.InternalErrorResponse(c, err)` - 500 response

### UUID Validation
Routes use `utils.ValidateUUID("paramName")` middleware for path params.

### GORM Hooks
Models can define lifecycle hooks that GORM calls automatically:
- `BeforeSave(tx *gorm.DB)` - Called before insert/update
- `BeforeCreate(tx *gorm.DB)` - Called before insert only
- `BeforeUpdate(tx *gorm.DB)` - Called before update only

Note: Hooks don't run on batch operations like `db.Model(&Task{}).Where(...).Update(...)`. Use individual record operations or update fields manually.

### Status Computation
Task and Milestone have a `Status` field computed from `Date` and `Completed`/`Reached`:
- `completed` → "completed"
- date in future → "not_started"
- date is today → "in_progress"
- date in past → "overdue"

Use `task.ComputeStatus()` or `milestone.ComputeStatus()` to recalculate.

### Async Job Processing
Background jobs (roadmap generation) use:
- `POST /api/v1/agent/generate-async` starts job, returns job_id
- `GET /ws/agent/:jobid` WebSocket for progress updates
- `GET /api/v1/jobs/:jobid/status` polling fallback
- Progress tracked via `types.ProgressUpdate` struct

### AI Sessions
Conversational roadmap generation:
- `POST /api/v1/agent/sessions` - Start session
- `POST /api/v1/agent/sessions/:id/reply` - Continue conversation
- `GET /api/v1/agent/sessions/:id` - Get session state

## Database

- Supabase PostgreSQL with RLS policies
- GORM ORM with PostgreSQL driver
- Run migrations: `supabase db push`
- Custom table names via `TableName()` methods
- `database.UpdateAllStatuses()` - Batch update task/milestone statuses (for daily jobs)
