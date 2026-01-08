# Habits System Implementation

This document describes the habits system that has been integrated into the Dead Reckoner server.

## Overview

The habits system provides comprehensive support for:
- **One-time todos** - Single tasks with a due date
- **Recurring habits** - Daily, weekly, or monthly recurring tasks
- **Completion tracking** - Track when habits are completed with optional notes
- **Statistics** - Streaks, completion rates, and analytics
- **Calendar views** - Query habits for specific dates or date ranges

## Architecture

### 1. Data Models (`types/types.go`)

#### Core Types

- **`RecurrenceType`** - Enum for recurrence patterns: `none`, `daily`, `weekly`, `monthly`
- **`DayOfWeek`** - Bitmask for representing days of the week (1=Sun, 2=Mon, ..., 64=Sat)
- **`Habit`** - Main habit model with scheduling, recurrence, and metadata
- **`HabitCompletion`** - Tracks when a habit was completed for a specific date

#### Request/Response Types

- **`CreateHabitRequest`** - Payload for creating habits with validation
- **`UpdateHabitRequest`** - Payload for updating habits (all fields optional)
- **`CompleteHabitRequest`** - Payload for marking habits complete
- **`HabitWithStatus`** - Habit with completion status for a specific date
- **`HabitStatsResponse`** - Statistics including streak and completion rates

#### Key Features

**Bitmask Days of Week:**
```go
// Common presets
Weekdays = Monday | Tuesday | Wednesday | Thursday | Friday  // 62
Weekends = Saturday | Sunday                                  // 65
EveryDay = All days                                           // 127
```

**Validation:**
- Weekly recurrence requires `days_of_week` (1-127)
- Monthly recurrence requires `day_of_month` (1-31)
- One-time todos require `due_date`

### 2. Database Layer (`database/habits.go`)

All database operations include comprehensive logging for debugging and monitoring.

#### CRUD Operations

- **`CreateHabit`** - Creates a new habit with user validation
- **`GetHabit`** - Retrieves a single habit by ID
- **`GetAllHabits`** - Lists all habits for a user (with optional archived)
- **`UpdateHabit`** - Updates an existing habit
- **`DeleteHabit`** - Deletes a habit (cascades to completions)
- **`ArchiveHabit`** - Soft-archives a habit

#### Query Operations

- **`GetHabitsForDate`** - Returns habits due on a specific date with completion status
- **`GetHabitsForDateRange`** - Returns habits for a date range (max 90 days)

#### Completion Operations

- **`CompleteHabit`** - Marks a habit complete for a specific date
- **`UncompleteHabit`** - Removes a completion
- **`GetCompletions`** - Retrieves completion history

#### Statistics Operations

- **`GetStreak`** - Calculates current streak
- **`GetCompletionRate`** - Calculates completion rate over N days
- **`GetTotalCompletions`** - Returns total completion count

### 3. API Handlers (`api/handlers/habits.go`)

All handlers include detailed logging at Info, Warn, and Error levels for observability.

#### CRUD Endpoints

```
POST   /api/v1/habits              - Create a new habit
GET    /api/v1/habits              - Get all habits (with ?include_archived=true)
GET    /api/v1/habits/:habitid     - Get a specific habit
PATCH  /api/v1/habits/:habitid     - Update a habit
DELETE /api/v1/habits/:habitid     - Delete a habit
POST   /api/v1/habits/:habitid/archive - Archive a habit
```

#### Query Endpoints

```
GET /api/v1/habits/today                  - Get today's habits with completion status
GET /api/v1/habits/date/:date             - Get habits for a specific date (YYYY-MM-DD)
GET /api/v1/habits/range?start=...&end=... - Get habits for a date range
```

#### Completion Endpoints

```
POST   /api/v1/habits/:habitid/complete          - Mark habit complete
DELETE /api/v1/habits/:habitid/complete?date=... - Remove completion
GET    /api/v1/habits/:habitid/completions       - Get completion history
```

#### Statistics Endpoints

```
GET /api/v1/habits/:habitid/stats - Get habit statistics
```

### 4. Database Schema (`supabase/migrations/20260107000000_add_habits_system.sql`)

#### Tables

**`habits`** - Main habits table
- Recurrence configuration (type, days, dates)
- Active period (start_date, end_date)
- Archived status
- Comprehensive validation constraints

**`habit_completions`** - Completion tracking
- Links to habits with cascade delete
- Prevents duplicate completions per date
- Stores completion timestamp and optional notes

#### Features

- Row-level security (RLS) policies for user isolation
- Indexes optimized for common queries
- Validation constraints for data integrity
- Helper functions for querying habits by date
- Automatic `updated_at` timestamp trigger

## Usage Examples

### Create a Daily Habit

```json
POST /api/v1/habits
{
  "title": "Morning meditation",
  "description": "10 minutes of mindfulness",
  "recurrence_type": "daily",
  "start_date": "2024-01-01T00:00:00Z"
}
```

### Create a Weekly Habit (Weekdays Only)

```json
POST /api/v1/habits
{
  "title": "Gym workout",
  "recurrence_type": "weekly",
  "days_of_week": 62,
  "start_date": "2024-01-01T00:00:00Z"
}
```

Days of week bitmask values:
- `62` = Monday through Friday (weekdays)
- `65` = Saturday and Sunday (weekends)
- `127` = All days

### Create a Monthly Habit

```json
POST /api/v1/habits
{
  "title": "Monthly review",
  "recurrence_type": "monthly",
  "day_of_month": 1,
  "start_date": "2024-01-01T00:00:00Z"
}
```

### Create a One-Time Todo

```json
POST /api/v1/habits
{
  "title": "Submit tax returns",
  "recurrence_type": "none",
  "due_date": "2024-04-15T00:00:00Z",
  "start_date": "2024-01-01T00:00:00Z"
}
```

### Complete a Habit

```json
POST /api/v1/habits/:habitid/complete
{
  "notes": "Felt great today!"
}
```

Or complete for a specific date:

```json
POST /api/v1/habits/:habitid/complete
{
  "date": "2024-01-15T00:00:00Z",
  "notes": "Made up for yesterday"
}
```

### Get Today's Habits

```
GET /api/v1/habits/today
```

Response includes completion status:

```json
[
  {
    "habit_id": "...",
    "title": "Morning meditation",
    "recurrence_type": "daily",
    "is_completed": true,
    "completed_at": "2024-01-15T08:30:00Z",
    ...
  }
]
```

### Get Habit Statistics

```
GET /api/v1/habits/:habitid/stats
```

Response:

```json
{
  "current_streak": 7,
  "completion_rate_7d": 0.857,
  "completion_rate_30d": 0.733,
  "total_completions": 42
}
```

## Logging

The implementation includes comprehensive logging at all layers:

### Database Layer
- **Info**: Successful operations, record counts
- **Warn**: Records not found, validation failures
- **Error**: Database errors, unexpected failures

### Handler Layer
- **Info**: Request processing, successful responses
- **Warn**: Validation errors, not found errors
- **Error**: Internal server errors

All log messages include:
- Operation name (e.g., "CreateHabit", "GetStreak")
- Relevant IDs (habit_id, user_id)
- Additional context (dates, counts, error details)

## Security

- **Row Level Security (RLS)** - Users can only access their own habits
- **User ID Validation** - All operations verify user ownership
- **Input Validation** - Comprehensive validation on create/update
- **UUID Validation** - Route-level UUID validation middleware

## Database Migration

To apply the database migration:

```bash
# Using Supabase CLI
supabase db push

# Or manually apply the migration file
# supabase/migrations/20260107000000_add_habits_system.sql
```

## Testing Checklist

- [ ] Create habits with all recurrence types
- [ ] Validate recurrence rules (weekly needs days, etc.)
- [ ] Complete habits for today and past dates
- [ ] Verify duplicate completion prevention
- [ ] Test streak calculation
- [ ] Test completion rates
- [ ] Test date range queries (calendar view)
- [ ] Verify RLS policies (users can't access others' habits)
- [ ] Test archiving and filtering
- [ ] Verify cascade deletion

## Future Enhancements

Potential improvements to consider:

1. **Reminders/Notifications** - Push notifications for due habits
2. **Habit Templates** - Pre-defined habit templates for common use cases
3. **Tags/Categories** - Organize habits by category
4. **Sharing** - Share habit progress with friends/accountability partners
5. **Analytics Dashboard** - Visualizations of progress over time
6. **Habit Chains** - Dependencies between habits
7. **Time-based habits** - Habits due at specific times, not just dates
8. **Flexible recurrence** - "Every 3 days", "First Monday of month", etc.

## Support

For issues or questions:
- Check logs for detailed error information
- Verify database migration was applied successfully
- Ensure user authentication is working correctly
- Review RLS policies if access issues occur
