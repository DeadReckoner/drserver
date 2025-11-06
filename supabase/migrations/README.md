# Database Migrations

This directory contains all database schema migrations for the application. Migrations are applied in chronological order based on their timestamp prefix.

## Naming Convention

Migrations follow the pattern: `YYYYMMDDHHMMSS_description.sql`

Example: `20251105030000_fix_friendships_constraints.sql`

## Key Migrations

### Friendships Table (`20250901145225_remote_schema.sql` + `20251105030000_fix_friendships_constraints.sql`)

#### Initial Schema
Creates the friendships table to manage friend requests between users with:
- `requester_id`: User who sent the friend request
- `addressee_id`: User who received the friend request
- `status`: Request state (pending/accepted/rejected/blocked)
- `restrictions`: JSONB field for future use (privacy settings, mutual contacts, etc.)

#### Problems Solved by Constraints Migration

**1. Unique Constraint (`friendships_unique_pair`)**
- **Problem**: Without this, users could create duplicate friend requests (User A → User B multiple times)
- **Solution**: Uses `LEAST/GREATEST` to create a normalized pair regardless of direction
- **Efficiency**: Prevents data duplication and confusion about which request is "active"
- **How it works**:
  ```sql
  LEAST(requester_id, addressee_id), GREATEST(requester_id, addressee_id)
  ```
  This ensures User A + User B always creates the same pair, whether A requests B or B requests A

**2. Status Check Constraint (`friendships_status_check`)**
- **Problem**: String fields accept any value, allowing typos like "accpted" or "peding"
- **Solution**: Database-level validation ensures only valid statuses
- **Efficiency**: Catches bugs at the database layer instead of application layer
- **Benefits**: Prevents bad data from ever entering the database

**3. Query Performance Indexes**

##### `idx_friendships_requester_status` & `idx_friendships_addressee_status`
- **Problem**: Finding "all friend requests I sent" requires scanning entire table
- **Solution**: B-tree indexes on (requester_id, status) and (addressee_id, status)
- **Efficiency**: O(1) lookups instead of O(n) table scans
- **Example query this optimizes**:
  ```sql
  SELECT * FROM friendships
  WHERE requester_id = 'user-uuid' AND status = 'pending'
  ```

##### `idx_friendships_accepted` (Partial Index)
- **Problem**: "Get all my friends" is the most common query but requires checking both directions
- **Solution**: Partial index only on accepted friendships
- **Efficiency**: Smaller index (only accepted rows), faster queries
- **Why partial**: 90% of queries care about accepted friends, not pending/rejected
- **Example query**:
  ```sql
  SELECT * FROM friendships
  WHERE (requester_id = 'user-uuid' OR addressee_id = 'user-uuid')
  AND status = 'accepted'
  ```

##### `idx_friendships_created_at`
- **Problem**: Sorting friend lists chronologically requires expensive sorting
- **Solution**: B-tree index on created_at with DESC order
- **Efficiency**: Results come back pre-sorted, no sort operation needed
- **Use case**: Showing "newest friends first" or paginating friend lists

## How Indexes Improve Efficiency

### Without Indexes (Sequential Scan)
```
Query: Find all friends of User A
Database: Reads ALL 1,000,000 rows → Filters → Returns 150 results
Time: ~2000ms
```

### With Indexes (Index Scan)
```
Query: Find all friends of User A
Database: Looks up User A in index → Jumps directly to 150 matching rows
Time: ~5ms
```

**400x faster!**

### Index Types Used

- **B-tree** (default): Best for equality and range queries, sorted data
- **Partial Index** (WHERE clause): Only indexes subset of rows, smaller and faster
- **Composite Index** (multiple columns): Optimizes queries filtering on multiple fields

## Performance Tips

1. **Composite indexes are directional**: `(requester_id, status)` helps queries filtering on requester_id OR both, but NOT just status alone

2. **Partial indexes save space**: The `accepted` partial index is ~10x smaller than indexing all statuses

3. **Index maintenance cost**: Each index speeds up reads but slows down writes slightly. We use strategic indexes only for common query patterns.

4. **Unique indexes serve dual purpose**: They enforce constraints AND speed up lookups

## Query Patterns Supported

```sql
-- ✅ Optimized: Find pending requests I sent
SELECT * FROM friendships
WHERE requester_id = $1 AND status = 'pending';

-- ✅ Optimized: Find all my friends (accepted both directions)
SELECT * FROM friendships
WHERE (requester_id = $1 OR addressee_id = $1)
AND status = 'accepted'
ORDER BY created_at DESC;

-- ✅ Optimized: Check if friendship exists between two users
SELECT * FROM friendships
WHERE LEAST(requester_id, addressee_id) = LEAST($1, $2)
AND GREATEST(requester_id, addressee_id) = GREATEST($1, $2);

-- ✅ Optimized: Count my total friends
SELECT COUNT(*) FROM friendships
WHERE (requester_id = $1 OR addressee_id = $1)
AND status = 'accepted';
```

## Other Migrations

### Messages & Threads (`20250901141043_add_messaging_tables.sql`)
Adds messaging functionality with thread-based conversations.

### Roadmaps (`20250726145835_add-roadmaps.sql`)
Adds roadmap feature for tracking user goals and progress.

### User Search (`20250916000000_add_user_search_policy.sql`)
Adds row-level security policies for user search functionality.

### Tasks (`20251105020333_remote_schema.sql`)
Links tasks to roadmaps and adds phone number to users.

## Running Migrations

Migrations are automatically applied by Supabase when:
1. Pushing to production
2. Running `supabase db push`
3. Running `supabase migration up`

## Creating New Migrations

```bash
# Generate timestamp
date +"%Y%m%d%H%M%S"

# Create file
touch supabase/migrations/$(date +"%Y%m%d%H%M%S")_description.sql
```

Always include:
- Comments explaining what and why
- Indexes for new tables
- Constraints for data integrity
- Grants for appropriate roles
