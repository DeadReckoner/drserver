# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
go run main.go                              # Start server on :8080
go test ./...                               # Run all tests
go test ./api -v                            # Run API tests with verbose output
go test ./api -run TestFunctionName -v      # Run single test
fly deploy                                  # Deploy to Fly.io
```

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
├── MacroMap (strategic overview)
│   └── Phases
│       └── MacroMilestones (with Spikes, Risks)
└── Milestones (tactical detail)
    └── Tasks
        └── Steps
```

## Key Patterns

### Handler Pattern
All handlers follow this pattern:
1. Get userID from context via `c.GetString("userID")`
2. Parse/validate request body
3. Call database function
4. Return JSON response with appropriate status

### UUID Validation
Routes use `utils.ValidateUUID("paramName")` middleware for path params.

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
