# Milestone Refactoring Plan: Merge MacroMilestones with Milestones

## Executive Summary

This refactoring consolidates two separate milestone types (MacroMilestone and Milestone) into a single unified Milestone type. This will significantly simplify the codebase while maintaining the two-stage generation process.

**Current State:** Two separate types with bidirectional linking
**Target State:** Single Milestone type under Phases with progressive task expansion

---

## Table of Contents

1. [Overview](#overview)
2. [Pre-Refactoring Checklist](#pre-refactoring-checklist)
3. [Phase 1: Database Migration](#phase-1-database-migration)
4. [Phase 2: Type Definitions](#phase-2-type-definitions)
5. [Phase 3: Database Functions](#phase-3-database-functions)
6. [Phase 4: Agent Changes](#phase-4-agent-changes)
7. [Phase 5: API Handlers](#phase-5-api-handlers)
8. [Phase 6: Testing](#phase-6-testing)
9. [Post-Refactoring Tasks](#post-refactoring-tasks)
10. [Rollback Plan](#rollback-plan)

---

## Overview

### Current Architecture

```
Roadmap
  ├─ MacroMap
  │    └─ Phase[]
  │         └─ MacroMilestone[]
  │              ├─ Spikes[]
  │              ├─ Risks[]
  │              └─ detailed_milestone_id → (links to Milestone)
  │
  └─ Milestone[]  (separate array on Roadmap)
       ├─ macro_milestone_id → (links back to MacroMilestone)
       └─ Task[]
            └─ Step[]
```

**Problems:**
- Two separate types for essentially the same concept
- Bidirectional linking (detailed_milestone_id ↔ macro_milestone_id)
- Complex to query and maintain
- Confusing data model

### Target Architecture

```
Roadmap
  └─ MacroMap
       └─ Phase[]
            └─ Milestone[]  (unified type)
                 ├─ Strategic fields (name, acceptance_test, duration ranges)
                 ├─ Tactical fields (day, reached, date)
                 ├─ is_expanded (boolean - are tasks generated?)
                 ├─ Spikes[]
                 ├─ Risks[]
                 └─ Task[]  (only present if is_expanded=true)
                      └─ Step[]
```

**Benefits:**
- Single milestone type
- Clear hierarchy: Phases → Milestones → Tasks
- No bidirectional links
- Progressive task generation (expand on demand)
- Simpler queries and code

### Generation Flow Changes

**Before:**
1. Generate MacroMap with Phases → MacroMilestones
2. Generate separate Milestones array with Tasks
3. Create bidirectional links between them

**After:**
1. Generate MacroMap with Phases → Milestones (strategic fields only)
2. Generate Tasks for FIRST milestone only (set is_expanded=true)
3. Other milestones remain unexpanded (can be expanded later)

---

## Pre-Refactoring Checklist

### ⚠️ CRITICAL: Create Backup

```bash
# Create a database backup BEFORE starting
pg_dump $DATABASE_URL > backup_before_milestone_refactor_$(date +%Y%m%d_%H%M%S).sql

# Create a git branch for this refactor
git checkout -b refactor/merge-milestones
git commit -m "Checkpoint before milestone refactor"
```

### Review Current State

- [ ] Document any existing AI-generated roadmaps in production
- [ ] Count how many MacroMilestones and Milestones exist
- [ ] Verify all tests pass before starting
- [ ] Note any custom queries or features that depend on current structure

```bash
# Run current tests
go test ./... -v

# Check for MacroMilestone references
grep -r "MacroMilestone" --include="*.go" .

# Check for detailed_milestone_id usage
grep -r "detailed_milestone_id" --include="*.go" .
```

---

## Phase 1: Database Migration

### Overview

Migrate the database schema to merge macro_milestones table into milestones table.

### Files to Create

- `supabase/migrations/20251113000000_merge_milestones.sql`

### Migration Steps

#### Step 1.1: Add New Columns to Milestones Table

```sql
-- Add strategic fields from macro_milestones to milestones
ALTER TABLE public.milestones
  ADD COLUMN IF NOT EXISTS phase_id uuid,
  ADD COLUMN IF NOT EXISTS name character varying,
  ADD COLUMN IF NOT EXISTS acceptance_test text,
  ADD COLUMN IF NOT EXISTS duration_days_min integer,
  ADD COLUMN IF NOT EXISTS duration_days_max integer,
  ADD COLUMN IF NOT EXISTS resources_forecast jsonb,
  ADD COLUMN IF NOT EXISTS order_index integer,
  ADD COLUMN IF NOT EXISTS is_expanded boolean DEFAULT false;
```

#### Step 1.2: Create Foreign Key Constraint

```sql
-- Link milestones to phases
ALTER TABLE public.milestones
  ADD CONSTRAINT milestones_phase_id_fkey
  FOREIGN KEY (phase_id)
  REFERENCES phases(phase_id)
  ON UPDATE CASCADE
  ON DELETE CASCADE;
```

#### Step 1.3: Migrate Existing Data

```sql
-- Copy macro_milestones that don't have detailed milestones yet
-- These become unexpanded milestones
INSERT INTO public.milestones (
  milestone_id,
  phase_id,
  roadmap_id,
  user_id,
  name,
  acceptance_test,
  duration_days_min,
  duration_days_max,
  resources_forecast,
  order_index,
  is_expanded,
  sub_goal,
  description,
  day,
  reached
)
SELECT
  mm.macro_milestone_id,
  mm.phase_id,
  r.roadmap_id,
  r.user_id,
  mm.name,
  mm.acceptance_test,
  mm.duration_days_min,
  mm.duration_days_max,
  mm.resources_forecast,
  mm.order_index,
  false, -- is_expanded = false (no tasks yet)
  '',    -- empty sub_goal
  '',    -- empty description
  0,     -- day placeholder
  false  -- not reached
FROM public.macro_milestones mm
INNER JOIN public.phases p ON mm.phase_id = p.phase_id
INNER JOIN public.macro_maps map ON p.macro_map_id = map.macro_map_id
INNER JOIN public.roadmaps r ON map.roadmap_id = r.roadmap_id
WHERE mm.detailed_milestone_id IS NULL;

-- Update existing milestones with strategic data from their linked macro_milestone
UPDATE public.milestones m
SET
  phase_id = mm.phase_id,
  name = mm.name,
  acceptance_test = mm.acceptance_test,
  duration_days_min = mm.duration_days_min,
  duration_days_max = mm.duration_days_max,
  resources_forecast = mm.resources_forecast,
  order_index = mm.order_index,
  is_expanded = true  -- these already have tasks
FROM public.macro_milestones mm
WHERE m.macro_milestone_id = mm.macro_milestone_id;
```

#### Step 1.4: Update Spikes and Risks Tables

```sql
-- Rename foreign key columns in spikes table
ALTER TABLE public.spikes
  RENAME COLUMN macro_milestone_id TO milestone_id;

-- Rename foreign key columns in risks table
ALTER TABLE public.risks
  RENAME COLUMN macro_milestone_id TO milestone_id;

-- Drop old foreign key constraints
ALTER TABLE public.spikes
  DROP CONSTRAINT IF EXISTS spikes_macro_milestone_id_fkey;

ALTER TABLE public.risks
  DROP CONSTRAINT IF EXISTS risks_macro_milestone_id_fkey;

-- Add new foreign key constraints
ALTER TABLE public.spikes
  ADD CONSTRAINT spikes_milestone_id_fkey
  FOREIGN KEY (milestone_id)
  REFERENCES milestones(milestone_id)
  ON UPDATE CASCADE
  ON DELETE CASCADE;

ALTER TABLE public.risks
  ADD CONSTRAINT risks_milestone_id_fkey
  FOREIGN KEY (milestone_id)
  REFERENCES milestones(milestone_id)
  ON UPDATE CASCADE
  ON DELETE CASCADE;
```

#### Step 1.5: Clean Up Old Columns and Tables

```sql
-- Remove linking column from milestones
ALTER TABLE public.milestones
  DROP COLUMN IF EXISTS macro_milestone_id;

-- Drop the macro_milestones table (data already migrated)
DROP TABLE IF EXISTS public.macro_milestones CASCADE;
```

#### Step 1.6: Add Constraints and Indexes

```sql
-- Make name required (after data migration)
ALTER TABLE public.milestones
  ALTER COLUMN name SET NOT NULL;

-- Allow sub_goal to be NULL (for unexpanded milestones)
ALTER TABLE public.milestones
  ALTER COLUMN sub_goal DROP NOT NULL;

-- Create index on phase_id for better query performance
CREATE INDEX IF NOT EXISTS milestones_phase_id_idx
  ON public.milestones(phase_id);

-- Create composite index for common queries
CREATE INDEX IF NOT EXISTS milestones_roadmap_phase_idx
  ON public.milestones(roadmap_id, phase_id, order_index);
```

### Verification Queries

After running migration, verify data integrity:

```sql
-- Count milestones by type
SELECT
  is_expanded,
  COUNT(*) as count
FROM milestones
GROUP BY is_expanded;

-- Check for orphaned milestones (missing phase_id)
SELECT COUNT(*)
FROM milestones
WHERE phase_id IS NULL;

-- Verify all spikes and risks have valid milestone references
SELECT
  (SELECT COUNT(*) FROM spikes WHERE milestone_id NOT IN (SELECT milestone_id FROM milestones)) as orphaned_spikes,
  (SELECT COUNT(*) FROM risks WHERE milestone_id NOT IN (SELECT milestone_id FROM milestones)) as orphaned_risks;

-- Check milestone hierarchy integrity
SELECT
  p.name as phase_name,
  COUNT(m.milestone_id) as milestone_count
FROM phases p
LEFT JOIN milestones m ON p.phase_id = m.phase_id
GROUP BY p.phase_id, p.name
ORDER BY p.order_index;
```

### Checklist

- [ ] Create migration file `20251113000000_merge_milestones.sql`
- [ ] Review migration SQL carefully
- [ ] Test migration on a development database first
- [ ] Run migration on production database
- [ ] Run verification queries
- [ ] Confirm no data loss

---

## Phase 2: Type Definitions

### File: `types/types.go`

### Step 2.1: Update Milestone Struct

**Location:** `types/types.go:163-177`

**Before:**
```go
type Milestone struct {
	MilestoneID string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"milestone_id"`
	RoadmapID   string    `gorm:"type:uuid" json:"roadmap_id"`
	UserID      string    `gorm:"type:uuid" json:"user_id"`
	SubGoal     string    `json:"sub_goal"`
	Description string    `json:"description"`
	Day         int       `json:"day"`
	Reached     bool      `json:"reached"`
	Date        time.Time `json:"date,omitempty"`

	MacroMilestoneID *string `gorm:"type:uuid" json:"macro_milestone_id,omitempty"`

	Tasks       []Task    `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"tasks,omitempty"`
}
```

**After:**
```go
type Milestone struct {
	// Primary keys and relationships
	MilestoneID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"milestone_id"`
	PhaseID     string `gorm:"type:uuid" json:"phase_id"` // NEW: belongs to Phase
	RoadmapID   string `gorm:"type:uuid" json:"roadmap_id"`
	UserID      string `gorm:"type:uuid" json:"user_id"`

	// Strategic fields (from MacroMilestone)
	Name              string  `json:"name"`                                      // NEW
	AcceptanceTest    string  `json:"acceptance_test"`                           // NEW
	DurationDaysMin   int     `json:"duration_days_min"`                         // NEW
	DurationDaysMax   int     `json:"duration_days_max"`                         // NEW
	ResourcesForecast *string `gorm:"type:jsonb" json:"resources_forecast,omitempty"` // NEW
	OrderIndex        int     `json:"order_index"`                               // NEW

	// Tactical fields (original Milestone fields)
	SubGoal     string    `json:"sub_goal,omitempty"`      // Now optional
	Description string    `json:"description,omitempty"`   // Now optional
	Day         int       `json:"day"`
	Reached     bool      `json:"reached"`
	Date        time.Time `json:"date,omitempty"`

	// Status
	IsExpanded bool `json:"is_expanded"` // NEW: Has tasks been generated?

	// Relationships
	Spikes []Spike `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"spikes,omitempty"`  // NEW
	Risks  []Risk  `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"risks,omitempty"`   // NEW
	Tasks  []Task  `gorm:"foreignKey:MilestoneID;constraint:OnDelete:CASCADE;" json:"tasks,omitempty"`
}
```

### Step 2.2: Update Phase Struct

**Location:** `types/types.go:116-124`

**Before:**
```go
type Phase struct {
	PhaseID         string            `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"phase_id"`
	MacroMapID      string            `gorm:"type:uuid" json:"macro_map_id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	OrderIndex      int               `json:"order_index"`
	DurationWeeks   string            `json:"duration_weeks"`
	MacroMilestones []MacroMilestone  `gorm:"foreignKey:PhaseID;constraint:OnDelete:CASCADE;" json:"macro_milestones,omitempty"`
}
```

**After:**
```go
type Phase struct {
	PhaseID       string      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"phase_id"`
	MacroMapID    string      `gorm:"type:uuid" json:"macro_map_id"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	OrderIndex    int         `json:"order_index"`
	DurationWeeks string      `json:"duration_weeks"`
	Milestones    []Milestone `gorm:"foreignKey:PhaseID;constraint:OnDelete:CASCADE;" json:"milestones,omitempty"` // CHANGED
}
```

### Step 2.3: Update Spike and Risk Structs

**Location:** `types/types.go:148-161`

**Before:**
```go
type Spike struct {
	SpikeID             string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"spike_id"`
	MacroMilestoneID    string `gorm:"type:uuid" json:"macro_milestone_id"`
	Description         string `json:"description"`
	TimeboxDays         int    `json:"timebox_days"`
}

type Risk struct {
	RiskID              string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"risk_id"`
	MacroMilestoneID    string `gorm:"type:uuid" json:"macro_milestone_id"`
	Description         string `json:"description"`
	Severity            string `json:"severity"`
}
```

**After:**
```go
type Spike struct {
	SpikeID      string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"spike_id"`
	MilestoneID  string `gorm:"type:uuid" json:"milestone_id"` // CHANGED
	Description  string `json:"description"`
	TimeboxDays  int    `json:"timebox_days"`
}

type Risk struct {
	RiskID       string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"risk_id"`
	MilestoneID  string `gorm:"type:uuid" json:"milestone_id"` // CHANGED
	Description  string `json:"description"`
	Severity     string `json:"severity"`
}
```

### Step 2.4: DELETE MacroMilestone Struct

**Location:** `types/types.go:127-145`

**Delete this entire struct** (and all related Input types like MacroMilestoneInput in openapi.yaml)

### Step 2.5: Update Roadmap Struct

**Location:** `types/types.go:91-106`

**Before:**
```go
type Roadmap struct {
	RoadmapID   string      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"roadmap_id"`
	UserID      string      `gorm:"type:uuid" json:"user_id"`
	Goal        string      `json:"goal"`
	Description string      `json:"description"`
	Days        int         `json:"days"`
	Completed   bool        `json:"completed"`
	StartDate   time.Time   `json:"start_date,omitempty"`
	Type        string      `gorm:"type:roadmap_type;default:'custom_made'" json:"type"`

	MacroMap    *MacroMap   `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"macro_map,omitempty"`
	Milestones  []Milestone `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"milestones,omitempty"`
}
```

**After:**
```go
type Roadmap struct {
	RoadmapID   string      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"roadmap_id"`
	UserID      string      `gorm:"type:uuid" json:"user_id"`
	Goal        string      `json:"goal"`
	Description string      `json:"description"`
	Days        int         `json:"days"`
	Completed   bool        `json:"completed"`
	StartDate   time.Time   `json:"start_date,omitempty"`
	Type        string      `gorm:"type:roadmap_type;default:'custom_made'" json:"type"`

	// Only MacroMap remains (milestones now live under Phases)
	MacroMap    *MacroMap   `gorm:"foreignKey:RoadmapID;constraint:OnDelete:CASCADE;" json:"macro_map,omitempty"`
	// REMOVED: Milestones array
}
```

### Checklist

- [ ] Update Milestone struct with all new fields
- [ ] Update Phase struct to reference Milestones instead of MacroMilestones
- [ ] Update Spike and Risk structs
- [ ] Delete MacroMilestone struct
- [ ] Update Roadmap struct
- [ ] Build the project to check for compilation errors: `go build`
- [ ] Fix any compilation errors before proceeding

---

## Phase 3: Database Functions

### File: `database/database.go`

### Step 3.1: Update generateUUIDs Function

**Location:** `database/database.go:177-223`

**Replace the entire function:**

```go
func generateUUIDs(rm *types.Roadmap) {
	rm.RoadmapID = uuid.NewString()

	if rm.MacroMap != nil {
		rm.MacroMap.MacroMapID = uuid.NewString()
		rm.MacroMap.RoadmapID = rm.RoadmapID

		for i := range rm.MacroMap.Phases {
			rm.MacroMap.Phases[i].PhaseID = uuid.NewString()
			rm.MacroMap.Phases[i].MacroMapID = rm.MacroMap.MacroMapID

			// Generate UUIDs for Milestones under Phases
			for j := range rm.MacroMap.Phases[i].Milestones {
				milestone := &rm.MacroMap.Phases[i].Milestones[j]
				milestone.MilestoneID = uuid.NewString()
				milestone.PhaseID = rm.MacroMap.Phases[i].PhaseID
				milestone.RoadmapID = rm.RoadmapID

				// Generate UUIDs for Spikes
				for k := range milestone.Spikes {
					milestone.Spikes[k].SpikeID = uuid.NewString()
					milestone.Spikes[k].MilestoneID = milestone.MilestoneID
				}

				// Generate UUIDs for Risks
				for k := range milestone.Risks {
					milestone.Risks[k].RiskID = uuid.NewString()
					milestone.Risks[k].MilestoneID = milestone.MilestoneID
				}

				// Generate UUIDs for Tasks (only if milestone is expanded)
				for k := range milestone.Tasks {
					task := &milestone.Tasks[k]
					task.TaskID = uuid.NewString()
					task.MilestoneID = milestone.MilestoneID
					task.RoadmapID = rm.RoadmapID

					// Generate UUIDs for Steps
					for l := range task.Steps {
						task.Steps[l].StepID = uuid.NewString()
						task.Steps[l].TaskID = task.TaskID
					}
				}
			}
		}
	}

	// REMOVED: Old milestones array processing
}
```

### Step 3.2: Update CreateRoadmap Function

**Location:** `database/database.go:225-281`

**Changes needed:**

```go
func CreateRoadmap(c *gin.Context, rm types.Roadmap) (types.Roadmap, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Roadmap{}, errors.New("missing or invalid user_id")
	}

	userIDStr := user_id.(string)

	// Ensure user exists (existing code - no change)
	var existingUser types.User
	userResult := db.Where("user_id = ?", userIDStr).First(&existingUser)
	if userResult.Error == gorm.ErrRecordNotFound {
		user := types.User{
			UserID:    userIDStr,
			CreatedAt: time.Now(),
		}
		if err := db.Create(&user).Error; err != nil {
			return types.Roadmap{}, errors.New("failed to create user: " + err.Error())
		}
	} else if userResult.Error != nil {
		return types.Roadmap{}, errors.New("failed to check user existence: " + userResult.Error.Error())
	}

	// Generate UUIDs for all entities and set foreign keys
	generateUUIDs(&rm)

	// Set user IDs for all nested entities
	rm.UserID = userIDStr

	// Set user IDs for milestones under phases
	if rm.MacroMap != nil {
		for i := range rm.MacroMap.Phases {
			for j := range rm.MacroMap.Phases[i].Milestones {
				rm.MacroMap.Phases[i].Milestones[j].UserID = userIDStr

				// Set user IDs for tasks (if expanded)
				for k := range rm.MacroMap.Phases[i].Milestones[j].Tasks {
					rm.MacroMap.Phases[i].Milestones[j].Tasks[k].UserID = userIDStr
				}
			}
		}
	}

	tx := db.Create(&rm)
	if tx.Error != nil {
		return types.Roadmap{}, tx.Error
	}

	// Load created roadmap with all associations
	var createdRoadmap types.Roadmap
	query := db.Where("roadmap_id = ?", rm.RoadmapID)

	if rm.MacroMap != nil {
		query = query.
			Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
				return db.Order("order_index ASC")
			}).
			Preload("MacroMap.Phases.Milestones", func(db *gorm.DB) *gorm.DB {
				return db.Order("order_index ASC")
			}).
			Preload("MacroMap.Phases.Milestones.Spikes").
			Preload("MacroMap.Phases.Milestones.Risks").
			Preload("MacroMap.Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
				return db.Order("day ASC")
			}).
			Preload("MacroMap.Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
				return db.Order("number ASC")
			})
	}

	if err := query.First(&createdRoadmap).Error; err != nil {
		return types.Roadmap{}, err
	}

	return createdRoadmap, nil
}
```

### Step 3.3: Update GetRoadmapWithMacroMap Function

**Location:** `database/database.go:306-331`

```go
func GetRoadmapWithMacroMap(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.Milestones.Spikes").
		Preload("MacroMap.Phases.Milestones.Risks").
		// NOTE: Don't load tasks - this is macro view only (strategic)
		First(&roadmap)

	if result.Error != nil {
		log.Println("Error fetching roadmap macro map:", result.Error)
	}

	return roadmap
}
```

### Step 3.4: Update GetRoadmapWithMacroMapAndMilestones Function

**Location:** `database/database.go:333-367`

```go
func GetRoadmapWithMacroMapAndMilestones(c *gin.Context, roadmapID string) types.Roadmap {
	var roadmap types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmap
	}

	result := db.Where("user_id = ?", user_id).Where("roadmap_id = ?", roadmapID).
		Preload("MacroMap.Phases", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.Milestones.Spikes").
		Preload("MacroMap.Phases.Milestones.Risks").
		Preload("MacroMap.Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("MacroMap.Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		First(&roadmap)

	if result.Error != nil {
		log.Println("Error fetching full roadmap:", result.Error)
	}

	return roadmap
}
```

### Step 3.5: Update GetRoadmapsByUser Function

**Location:** `database/database.go:283-304`

**Remove or update Milestones preloading:**

```go
func GetRoadmapsByUser(c *gin.Context) []types.Roadmap {
	var roadmaps []types.Roadmap
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		log.Println("missing or invalid user_id")
		return roadmaps
	}

	// Load roadmaps with full macro map
	result := db.Where("user_id = ?", user_id).
		Preload("MacroMap.Phases.Milestones", func(db *gorm.DB) *gorm.DB {
			return db.Order("order_index ASC")
		}).
		Preload("MacroMap.Phases.Milestones.Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("MacroMap.Phases.Milestones.Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		Find(&roadmaps)

	if result.Error != nil {
		log.Println("Error fetching roadmaps:", result.Error)
	}

	return roadmaps
}
```

### Step 3.6: Add New Database Functions

Add these new functions to support milestone expansion:

```go
// GetMilestoneWithDetails fetches a single milestone with all its relationships
func GetMilestoneWithDetails(c *gin.Context, milestoneID string) (types.Milestone, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}, errors.New("missing or invalid user_id")
	}

	var milestone types.Milestone
	result := db.Where("user_id = ?", user_id).
		Where("milestone_id = ?", milestoneID).
		Preload("Spikes").
		Preload("Risks").
		Preload("Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("day ASC")
		}).
		Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		First(&milestone)

	if result.Error != nil {
		return types.Milestone{}, result.Error
	}

	return milestone, nil
}

// ExpandMilestone adds tasks to a milestone that hasn't been expanded yet
func ExpandMilestone(c *gin.Context, milestoneID string, tasks []types.Task) (types.Milestone, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return types.Milestone{}, errors.New("missing or invalid user_id")
	}

	// Get the milestone
	var milestone types.Milestone
	result := db.Where("user_id = ?", user_id).
		Where("milestone_id = ?", milestoneID).
		First(&milestone)

	if result.Error != nil {
		return types.Milestone{}, result.Error
	}

	// Verify milestone hasn't been expanded already
	if milestone.IsExpanded {
		return types.Milestone{}, errors.New("milestone already expanded")
	}

	// Generate UUIDs for tasks and link them
	for i := range tasks {
		tasks[i].TaskID = uuid.NewString()
		tasks[i].MilestoneID = milestoneID
		tasks[i].RoadmapID = milestone.RoadmapID
		tasks[i].UserID = user_id.(string)

		for j := range tasks[i].Steps {
			tasks[i].Steps[j].StepID = uuid.NewString()
			tasks[i].Steps[j].TaskID = tasks[i].TaskID
		}
	}

	// Create all tasks in a transaction
	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Create(&tasks).Error; err != nil {
		tx.Rollback()
		return types.Milestone{}, err
	}

	// Mark milestone as expanded
	if err := tx.Model(&milestone).Update("is_expanded", true).Error; err != nil {
		tx.Rollback()
		return types.Milestone{}, err
	}

	if err := tx.Commit().Error; err != nil {
		return types.Milestone{}, err
	}

	// Reload milestone with tasks
	return GetMilestoneWithDetails(c, milestoneID)
}

// GetMilestonesByPhase returns all milestones for a phase
func GetMilestonesByPhase(c *gin.Context, phaseID string, includeTasks bool) ([]types.Milestone, error) {
	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		return nil, errors.New("missing or invalid user_id")
	}

	query := db.Where("user_id = ?", user_id).
		Where("phase_id = ?", phaseID).
		Order("order_index ASC").
		Preload("Spikes").
		Preload("Risks")

	if includeTasks {
		query = query.
			Preload("Tasks", func(db *gorm.DB) *gorm.DB {
				return db.Order("day ASC")
			}).
			Preload("Tasks.Steps", func(db *gorm.DB) *gorm.DB {
				return db.Order("number ASC")
			})
	}

	var milestones []types.Milestone
	if err := query.Find(&milestones).Error; err != nil {
		return nil, err
	}

	return milestones, nil
}
```

### Step 3.7: Update shouldCompleteRoadmap Function

**Location:** `database/database.go:956-961`

**This function needs to be updated** to check milestones under phases:

```go
// shouldCompleteRoadmap checks if all milestones in a roadmap are completed
func shouldCompleteRoadmap(roadmapID string) bool {
	// Get all phases for this roadmap
	var macroMap types.MacroMap
	if err := db.Where("roadmap_id = ?", roadmapID).First(&macroMap).Error; err != nil {
		return false
	}

	// Count incomplete expanded milestones across all phases
	var incompleteMilestones int64
	db.Model(&types.Milestone{}).
		Joins("INNER JOIN phases ON milestones.phase_id = phases.phase_id").
		Where("phases.macro_map_id = ? AND milestones.is_expanded = ? AND milestones.reached = ?",
			macroMap.MacroMapID, true, false).
		Count(&incompleteMilestones)

	return incompleteMilestones == 0
}
```

### Checklist

- [ ] Update generateUUIDs function
- [ ] Update CreateRoadmap function
- [ ] Update GetRoadmapWithMacroMap function
- [ ] Update GetRoadmapWithMacroMapAndMilestones function
- [ ] Update GetRoadmapsByUser function
- [ ] Add GetMilestoneWithDetails function
- [ ] Add ExpandMilestone function
- [ ] Add GetMilestonesByPhase function
- [ ] Update shouldCompleteRoadmap function
- [ ] Test compilation: `go build`

---

## Phase 4: Agent Changes

### File: `agent/agent.go`

### Step 4.1: Update generateMacroMap Schema

**Location:** `agent/agent.go:152-308`

**Key change:** Rename `macro_milestones` to `milestones` in the schema and add `is_expanded` field.

**Replace the `milestones` property in the schema:**

```go
// Inside the phase properties:
"milestones": map[string]interface{}{  // RENAMED from "macro_milestones"
	"type": "array",
	"items": map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{"type": "string"},
			"acceptance_test": map[string]interface{}{"type": "string"},
			"duration_days_min": map[string]interface{}{"type": "integer"},
			"duration_days_max": map[string]interface{}{"type": "integer"},
			"order_index": map[string]interface{}{"type": "integer"},
			"resources_forecast": map[string]interface{}{"type": "string"},
			"is_expanded": map[string]interface{}{
				"type": "boolean",
				"const": false,  // Always false in stage 1
			},
			"spikes": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": map[string]interface{}{"type": "string"},
						"timebox_days": map[string]interface{}{"type": "integer"},
					},
					"required": []string{"description", "timebox_days"},
					"additionalProperties": false,
				},
			},
			"risks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": map[string]interface{}{"type": "string"},
						"severity": map[string]interface{}{
							"type": "string",
							"enum": []string{"low", "medium", "high"},
						},
					},
					"required": []string{"description", "severity"},
					"additionalProperties": false,
				},
			},
		},
		"required": []string{"name", "acceptance_test", "duration_days_min", "duration_days_max", "order_index", "resources_forecast", "is_expanded", "spikes", "risks"},
		"additionalProperties": false,
	},
},
```

**Update the system prompt:**

```go
systemPrompt := `You are a Goal Achievement Coach Agent focused on creating strategic roadmaps.

CORE DIRECTIVES:
- Be autonomous: Make informed assumptions rather than asking for clarification
- Be thorough: Create comprehensive, actionable plans
- Be realistic: Provide achievable timelines with buffer for learning curves
- Be empowering: Frame guidance as enabling the user, not prescribing

YOUR TASK - STRATEGIC PLANNING (MACRO MAP):
Generate a high-level strategic roadmap (macro map) with 2-4 major phases.

For each phase, create 2-5 milestones that represent significant achievements. Each milestone must include:
- name: Clear milestone name
- acceptance_test: How to know it's complete
- duration_days_min/max: Duration estimates (accounting for learning curves)
- resources_forecast: Tools, materials, courses needed as JSON string
- is_expanded: Must be false (tasks will be generated separately)
- spikes: Time-boxed investigations (empty array if none)
- risks: Potential obstacles with severity (empty array if none)

IMPORTANT: Set is_expanded to false for all milestones. Tasks will be generated in a separate step.

STRATEGIC PRINCIPLES:
1. Start with fundamentals before advanced concepts
2. Build milestones that validate progress before moving forward
3. Include learning time - people need to absorb and practice
4. Anticipate common obstacles and plan mitigations
5. Balance ambition with achievability`
```

### Step 4.2: Update generateMilestoneMap Function

**Location:** `agent/agent.go:319-507`

**This function now generates tasks for ONE specific milestone ID.**

**New signature:**

```go
func generateMilestoneMap(ctx context.Context, userPrompt string, macroMap *types.MacroMap, milestoneID string) ([]byte, error)
```

**Find the target milestone:**

```go
// Find the specific milestone to expand
var targetMilestone *types.Milestone
var targetPhase *types.Phase

for i := range macroMap.Phases {
	for j := range macroMap.Phases[i].Milestones {
		if macroMap.Phases[i].Milestones[j].MilestoneID == milestoneID {
			targetMilestone = &macroMap.Phases[i].Milestones[j]
			targetPhase = &macroMap.Phases[i]
			break
		}
	}
	if targetMilestone != nil {
		break
	}
}

if targetMilestone == nil {
	return nil, fmt.Errorf("milestone %s not found in macro map", milestoneID)
}
```

**Update schema to generate only tasks:**

```go
// Schema now generates ONLY tasks array (not a full roadmap/milestone)
schema := map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"tasks": map[string]interface{}{
			"type":     "array",
			"minItems": 10,
			"maxItems": 14,
			"items": map[string]interface{}{
				// ... same task schema as before
			},
		},
	},
	"required":             []string{"tasks"},
	"additionalProperties": false,
}
```

**Update system prompt:**

```go
systemPrompt := fmt.Sprintf(`You are a Goal Achievement Coach Agent focused on creating detailed, actionable tactical plans.

PHASE CONTEXT:
Phase: %s
Description: %s

MILESTONE TO EXPAND:
Name: %s
Acceptance Test: %s
Duration: %d-%d days

%s

YOUR TASK - EXPAND THIS MILESTONE:
Generate 10-14 specific, actionable tasks for the milestone "%s".

These tasks must build logically toward achieving: "%s"

TASK REQUIREMENTS:
- Progress logically (each building on previous work)
- Mix learning with practice/application
- Include validation checkpoints
- Spread across %d-%d days
- Each task needs: title, description, rationale, est_minutes, dependencies, confidence, day, steps

TASK DESIGN PRINCIPLES:
1. Start with foundational/easier tasks, progress to more complex
2. Include both "learning" and "doing" tasks
3. Build in practice and repetition for skill development
4. Add checkpoint/validation tasks every 3-4 tasks
5. Make dependencies realistic - don't over-constrain
6. Account for spikes and risks in task planning`,
	targetPhase.Name,
	targetPhase.Description,
	targetMilestone.Name,
	targetMilestone.AcceptanceTest,
	targetMilestone.DurationDaysMin,
	targetMilestone.DurationDaysMax,
	buildMilestoneContext(targetMilestone),
	targetMilestone.Name,
	targetMilestone.AcceptanceTest,
	targetMilestone.DurationDaysMin,
	targetMilestone.DurationDaysMax,
)
```

### Step 4.3: Add buildMilestoneContext Helper

```go
func buildMilestoneContext(milestone *types.Milestone) string {
	context := ""

	if len(milestone.Spikes) > 0 {
		context += "Spikes to address: "
		for _, spike := range milestone.Spikes {
			context += fmt.Sprintf("\n  - %s (%d days)", spike.Description, spike.TimeboxDays)
		}
		context += "\n"
	}

	if len(milestone.Risks) > 0 {
		context += "Risks to mitigate: "
		for _, risk := range milestone.Risks {
			context += fmt.Sprintf("\n  - %s [%s]", risk.Description, risk.Severity)
		}
		context += "\n"
	}

	if milestone.ResourcesForecast != nil && *milestone.ResourcesForecast != "" {
		context += fmt.Sprintf("Resources available: %s\n", *milestone.ResourcesForecast)
	}

	return context
}
```

### Step 4.4: Update generateRoadmapFromPrompt Function

**Location:** `agent/agent.go:104-139`

**Major changes to the flow:**

```go
func generateRoadmapFromPrompt(ctx context.Context, userPrompt string) (*GeneratedRoadmap, error) {
	log.Info(ctx, "User Prompt", userPrompt)

	// STEP 1: Generate Macro Map with all Milestones (strategic view)
	macroMapResp, err := generateMacroMap(ctx, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate macro map: %w", err)
	}

	var macroMapData types.MacroMap
	if err := parseMacroMapResponse(macroMapResp, &macroMapData); err != nil {
		return nil, fmt.Errorf("failed to parse macro map: %w", err)
	}

	log.Info(ctx, "Macro Map", macroMapData)

	// STEP 2: Find the first milestone to expand
	if len(macroMapData.Phases) == 0 || len(macroMapData.Phases[0].Milestones) == 0 {
		return nil, fmt.Errorf("no milestones found in macro map")
	}

	firstMilestone := &macroMapData.Phases[0].Milestones[0]
	firstMilestone.IsExpanded = false // Will be set to true after tasks are generated

	// STEP 3: Generate tasks for the first milestone
	tasksResp, err := generateMilestoneMap(ctx, userPrompt, &macroMapData, firstMilestone.MilestoneID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tasks: %w", err)
	}

	// Parse tasks response
	var tasksData struct {
		Tasks []types.Task `json:"tasks"`
	}
	if err := json.Unmarshal(tasksResp, &tasksData); err != nil {
		// Try unwrapping OpenAI response format
		var apiResp types.OpenAIResponse
		if err := json.Unmarshal(tasksResp, &apiResp); err == nil {
			if len(apiResp.Output) > 0 {
				for _, output := range apiResp.Output {
					if output.Type == "message" && len(output.Content) > 0 {
						rawJSON := output.Content[0].Text
						if err := json.Unmarshal([]byte(rawJSON), &tasksData); err != nil {
							return nil, fmt.Errorf("failed to parse tasks JSON: %w", err)
						}
						break
					}
				}
			}
		} else {
			return nil, fmt.Errorf("failed to parse tasks response: %w", err)
		}
	}

	// Attach tasks to the first milestone
	macroMapData.Phases[0].Milestones[0].Tasks = tasksData.Tasks
	macroMapData.Phases[0].Milestones[0].IsExpanded = true

	// Build roadmap structure
	roadmap := &types.Roadmap{
		Goal:        userPrompt,
		Description: "AI-generated roadmap",
		MacroMap:    &macroMapData,
	}

	// Calculate total days from all milestones
	totalDays := 0
	for _, phase := range macroMapData.Phases {
		for _, milestone := range phase.Milestones {
			totalDays += milestone.DurationDaysMax
		}
	}
	roadmap.Days = totalDays

	return &GeneratedRoadmap{
		MacroMap: &macroMapData,
		Roadmap:  roadmap,
	}, nil
}
```

### Step 4.5: Remove buildMacroContext (No Longer Needed)

**Location:** `agent/agent.go:516-553`

**DELETE this function** - it's replaced by `buildMilestoneContext`

### Checklist

- [ ] Update generateMacroMap schema and prompts
- [ ] Update generateMilestoneMap function signature and logic
- [ ] Add buildMilestoneContext helper
- [ ] Update generateRoadmapFromPrompt flow
- [ ] Remove old buildMacroContext function
- [ ] Test compilation: `go build`

---

## Phase 5: API Handlers

### File: `api/handlers/agent.go`

### Step 5.1: Update AgentGenerate Handler

**Location:** `api/handlers/agent.go:19-53`

**Should work without changes, but verify the response structure:**

```go
func AgentGenerate(c *gin.Context) {
	ctx := c.Request.Context()
	var goalReq types.GoalRequest

	if !utils.BindJSONOrAbort(c, &goalReq) {
		return
	}

	fmt.Printf("Goal request received: %+v\n", goalReq)

	start := time.Now()
	log.Info(ctx, "Start Time:", start)

	// Generate macro map with milestones + tasks for first milestone
	generated, err := agent.GenerateGoalFromStructure(ctx, goalReq)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	fmt.Printf("Generated roadmap with macro map: %+v\n", generated)

	// Set roadmap type
	generated.Roadmap.Type = "ai_generated"
	generated.Roadmap.MacroMap = generated.MacroMap

	utils.DataResponse(c, http.StatusOK, generated.Roadmap)
	log.Info(ctx, "Roadmap generated successfully")
}
```

### Step 5.2: Add ExpandMilestone Handler

**Add new handler to `api/handlers/roadmap.go`:**

```go
// ExpandMilestone generates tasks for a milestone that hasn't been expanded yet
func ExpandMilestone(c *gin.Context) {
	ctx := c.Request.Context()
	milestoneID := c.Param("milestoneid")

	// Get the milestone
	milestone, err := database.GetMilestoneWithDetails(c, milestoneID)
	if err != nil {
		utils.NotFoundResponse(c, "Milestone")
		return
	}

	// Check if already expanded
	if milestone.IsExpanded {
		utils.BadRequestResponse(c, "Milestone already has tasks generated")
		return
	}

	// Get the roadmap and macro map for context
	roadmap := database.GetRoadmapWithMacroMap(c, milestone.RoadmapID)
	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	// Find the goal description (for agent prompt)
	goalPrompt := roadmap.Goal
	if goalPrompt == "" {
		goalPrompt = "Continue working on your goal"
	}

	// Generate tasks using the agent
	tasksResp, err := agent.GenerateMilestoneMap(ctx, goalPrompt, roadmap.MacroMap, milestoneID)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	// Parse tasks
	var tasksData struct {
		Tasks []types.Task `json:"tasks"`
	}
	if err := json.Unmarshal(tasksResp, &tasksData); err != nil {
		// Try unwrapping OpenAI response
		var apiResp types.OpenAIResponse
		if err := json.Unmarshal(tasksResp, &apiResp); err == nil {
			if len(apiResp.Output) > 0 {
				for _, output := range apiResp.Output {
					if output.Type == "message" && len(output.Content) > 0 {
						rawJSON := output.Content[0].Text
						if err := json.Unmarshal([]byte(rawJSON), &tasksData); err != nil {
							utils.InternalErrorResponse(c, err)
							return
						}
						break
					}
				}
			}
		} else {
			utils.InternalErrorResponse(c, err)
			return
		}
	}

	// Save tasks and mark milestone as expanded
	expandedMilestone, err := database.ExpandMilestone(c, milestoneID, tasksData.Tasks)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, expandedMilestone)
	log.Info(ctx, "Milestone expanded successfully")
}
```

### File: `api/handlers/roadmap.go`

### Step 5.3: Update GetRoadmap Handler

**Location:** `api/handlers/roadmap.go:50-77`

**Update view query logic:**

```go
func GetRoadmap(c *gin.Context) {
	roadmapID := c.Param("roadmapid")
	view := c.DefaultQuery("view", "full")

	var roadmap types.Roadmap

	switch view {
	case "macro":
		// Load phases with milestones (strategic view, no tasks)
		roadmap = database.GetRoadmapWithMacroMap(c, roadmapID)
	case "full", "milestones":
		// "milestones" deprecated but supported for backward compatibility
		// Load everything (phases, milestones, tasks)
		roadmap = database.GetRoadmapWithMacroMapAndMilestones(c, roadmapID)
	default:
		utils.BadRequestResponse(c, "Invalid view parameter. Use 'macro' or 'full'")
		return
	}

	if roadmap.RoadmapID == "" {
		utils.NotFoundResponse(c, "Roadmap")
		return
	}

	utils.DataResponse(c, http.StatusOK, roadmap)
}
```

### Step 5.4: Add GetMilestonesByPhase Handler

**Add to `api/handlers/roadmap.go`:**

```go
// GetMilestonesByPhase returns all milestones for a specific phase
func GetMilestonesByPhase(c *gin.Context) {
	phaseID := c.Param("phaseid")
	includeTasks := c.DefaultQuery("include_tasks", "true") == "true"

	milestones, err := database.GetMilestonesByPhase(c, phaseID, includeTasks)
	if err != nil {
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, milestones)
}
```

### Step 5.5: Update Routes in main.go

**Add new routes:**

```go
// Milestone expansion endpoint
roadmaps.POST("/milestones/:milestoneid/expand", handlers.ExpandMilestone)

// Get milestones by phase
roadmaps.GET("/phases/:phaseid/milestones", handlers.GetMilestonesByPhase)
```

### Checklist

- [ ] Update AgentGenerate handler (verify, likely no changes needed)
- [ ] Add ExpandMilestone handler
- [ ] Update GetRoadmap handler view logic
- [ ] Add GetMilestonesByPhase handler
- [ ] Update routes in main.go
- [ ] Test compilation: `go build`

---

## Phase 6: Testing

### Unit Tests

Create test file: `database/database_test.go`

```go
package database

import (
	"testing"
	"dr/server/types"
	"github.com/stretchr/testify/assert"
)

func TestMilestoneExpansion(t *testing.T) {
	// TODO: Add tests for:
	// - Creating roadmap with unexpanded milestones
	// - Expanding a milestone
	// - Preventing double expansion
	// - Querying by phase
}
```

### Integration Tests

#### Test 1: Create AI-Generated Roadmap

```bash
curl -X POST http://localhost:8080/api/agent/generate \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "goalName": "Learn Piano",
    "goalDescription": "Learn to play basic piano pieces",
    "targetCompletionDate": "3 months",
    "weeklyTimeCommitment": "5 hours",
    "workFrequency": "Daily",
    "currentSkillLevel": "Beginner"
  }'
```

**Verify:**
- [ ] Response includes MacroMap with Phases
- [ ] Each Phase has Milestones
- [ ] First milestone has `is_expanded: true` and Tasks
- [ ] Other milestones have `is_expanded: false` and no Tasks
- [ ] Spikes and Risks are present on milestones

#### Test 2: Get Roadmap with Macro View

```bash
curl -X GET "http://localhost:8080/api/roadmaps/$ROADMAP_ID?view=macro" \
  -H "Authorization: Bearer $TOKEN"
```

**Verify:**
- [ ] Returns Phases and Milestones
- [ ] Does NOT include Tasks (even for expanded milestones)

#### Test 3: Get Roadmap with Full View

```bash
curl -X GET "http://localhost:8080/api/roadmaps/$ROADMAP_ID?view=full" \
  -H "Authorization: Bearer $TOKEN"
```

**Verify:**
- [ ] Returns Phases, Milestones, and Tasks
- [ ] First milestone has Tasks
- [ ] Other milestones don't have Tasks

#### Test 4: Expand a Milestone

```bash
curl -X POST "http://localhost:8080/api/milestones/$MILESTONE_ID/expand" \
  -H "Authorization: Bearer $TOKEN"
```

**Verify:**
- [ ] Returns milestone with tasks
- [ ] Milestone now has `is_expanded: true`
- [ ] Tasks are correctly linked

#### Test 5: Prevent Double Expansion

```bash
# Try expanding the same milestone again
curl -X POST "http://localhost:8080/api/milestones/$MILESTONE_ID/expand" \
  -H "Authorization: Bearer $TOKEN"
```

**Verify:**
- [ ] Returns 400 Bad Request
- [ ] Error message: "Milestone already has tasks generated"

#### Test 6: Get Milestones by Phase

```bash
curl -X GET "http://localhost:8080/api/phases/$PHASE_ID/milestones" \
  -H "Authorization: Bearer $TOKEN"
```

**Verify:**
- [ ] Returns all milestones for that phase
- [ ] Includes tasks for expanded milestones

#### Test 7: Task Completion Cascade

```bash
# Complete all tasks in a milestone
# Verify milestone auto-completes
# Verify roadmap auto-completes when all milestones done
```

**Verify:**
- [ ] Milestone marked as reached when all tasks complete
- [ ] Roadmap marked as complete when all expanded milestones complete

### Data Migration Verification

**After migration, run these queries:**

```sql
-- Check milestone counts
SELECT
  'Total milestones' as metric,
  COUNT(*) as count
FROM milestones
UNION ALL
SELECT
  'Expanded milestones',
  COUNT(*)
FROM milestones
WHERE is_expanded = true
UNION ALL
SELECT
  'Unexpanded milestones',
  COUNT(*)
FROM milestones
WHERE is_expanded = false;

-- Check for data integrity issues
SELECT
  'Milestones with NULL phase_id' as issue,
  COUNT(*) as count
FROM milestones
WHERE phase_id IS NULL
UNION ALL
SELECT
  'Milestones with NULL name',
  COUNT(*)
FROM milestones
WHERE name IS NULL
UNION ALL
SELECT
  'Orphaned spikes',
  COUNT(*)
FROM spikes
WHERE milestone_id NOT IN (SELECT milestone_id FROM milestones)
UNION ALL
SELECT
  'Orphaned risks',
  COUNT(*)
FROM risks
WHERE milestone_id NOT IN (SELECT milestone_id FROM milestones);

-- Verify hierarchy
SELECT
  r.roadmap_id,
  r.goal,
  p.name as phase_name,
  COUNT(m.milestone_id) as milestone_count,
  SUM(CASE WHEN m.is_expanded THEN 1 ELSE 0 END) as expanded_count
FROM roadmaps r
JOIN macro_maps mm ON r.roadmap_id = mm.roadmap_id
JOIN phases p ON mm.macro_map_id = p.macro_map_id
LEFT JOIN milestones m ON p.phase_id = m.phase_id
GROUP BY r.roadmap_id, r.goal, p.phase_id, p.name
ORDER BY r.roadmap_id, p.order_index;
```

### Performance Testing

```bash
# Test loading a roadmap with many phases/milestones
time curl -X GET "http://localhost:8080/api/roadmaps/$ROADMAP_ID?view=full" \
  -H "Authorization: Bearer $TOKEN"
```

**Verify:**
- [ ] Response time < 1 second for typical roadmap
- [ ] No N+1 query issues (check database logs)

### Checklist

- [ ] All unit tests pass
- [ ] Integration test 1: Create AI roadmap ✓
- [ ] Integration test 2: Get macro view ✓
- [ ] Integration test 3: Get full view ✓
- [ ] Integration test 4: Expand milestone ✓
- [ ] Integration test 5: Prevent double expansion ✓
- [ ] Integration test 6: Get by phase ✓
- [ ] Integration test 7: Completion cascade ✓
- [ ] Data migration verified ✓
- [ ] Performance acceptable ✓

---

## Post-Refactoring Tasks

### Code Cleanup

- [ ] Remove all references to MacroMilestone type
- [ ] Remove detailed_milestone_id and macro_milestone_id references
- [ ] Update OpenAPI/Swagger documentation
- [ ] Update any frontend types/interfaces

### Documentation

- [ ] Update README with new architecture
- [ ] Document new API endpoints
- [ ] Update API examples
- [ ] Create migration guide for frontend developers

### Optional Enhancements

- [ ] Add endpoint to expand ALL milestones in a phase
- [ ] Add endpoint to get next unexpanded milestone
- [ ] Add progress indicators (X of Y milestones expanded)
- [ ] Add ability to regenerate tasks for a milestone

---

## Rollback Plan

### If Issues Occur During Migration

#### Step 1: Stop Application

```bash
# Stop the Go server
pkill -f "go run main.go"
```

#### Step 2: Restore Database

```bash
# Restore from backup
psql $DATABASE_URL < backup_before_milestone_refactor_YYYYMMDD_HHMMSS.sql
```

#### Step 3: Revert Code

```bash
# Revert to previous commit
git reset --hard HEAD~1

# Or checkout previous branch
git checkout main
```

#### Step 4: Verify Restoration

```bash
# Run old tests
go test ./... -v

# Test old API endpoints
curl -X GET "http://localhost:8080/api/roadmaps/$ROADMAP_ID" \
  -H "Authorization: Bearer $TOKEN"
```

### If Issues Occur After Deployment

#### Option 1: Hotfix Forward

- Identify the specific bug
- Create a quick fix
- Deploy hotfix

#### Option 2: Rollback and Fix Later

- Revert to previous version
- Fix issues in development
- Re-deploy when ready

---

## Summary Checklist

### Pre-Refactoring
- [ ] Database backup created
- [ ] Git branch created
- [ ] Current tests passing
- [ ] Dependencies documented

### Phase 1: Database
- [ ] Migration file created
- [ ] Migration tested on dev database
- [ ] Migration run on production
- [ ] Data integrity verified

### Phase 2: Types
- [ ] Milestone struct updated
- [ ] Phase struct updated
- [ ] Spike/Risk structs updated
- [ ] MacroMilestone deleted
- [ ] Roadmap struct updated
- [ ] Code compiles

### Phase 3: Database Functions
- [ ] generateUUIDs updated
- [ ] CreateRoadmap updated
- [ ] Get functions updated
- [ ] New helper functions added
- [ ] Code compiles

### Phase 4: Agent
- [ ] generateMacroMap updated
- [ ] generateMilestoneMap updated
- [ ] Helper functions updated
- [ ] Generation flow updated
- [ ] Code compiles

### Phase 5: Handlers
- [ ] AgentGenerate verified
- [ ] ExpandMilestone added
- [ ] GetRoadmap updated
- [ ] New endpoints added
- [ ] Routes updated
- [ ] Code compiles

### Phase 6: Testing
- [ ] All tests passing
- [ ] Integration tests complete
- [ ] Data migration verified
- [ ] Performance acceptable

### Post-Refactoring
- [ ] Code cleanup complete
- [ ] Documentation updated
- [ ] Frontend notified
- [ ] Deployment successful

---

## Estimated Timeline

- **Phase 1 (Database):** 2-3 hours
- **Phase 2 (Types):** 1 hour
- **Phase 3 (Database Functions):** 3-4 hours
- **Phase 4 (Agent):** 2-3 hours
- **Phase 5 (Handlers):** 1-2 hours
- **Phase 6 (Testing):** 2-3 hours
- **Documentation:** 1-2 hours

**Total:** 12-18 hours

---

## Notes

- Take breaks between phases
- Commit after each major phase completion
- Test thoroughly before moving to next phase
- Keep old backup until confident in new system
- Monitor production closely after deployment

---

## Questions to Resolve

- [ ] Should we auto-expand next milestone when current one completes?
- [ ] Should we allow users to manually expand any milestone?
- [ ] Should we keep historical task generation (versioning)?
- [ ] Do we need to support regenerating tasks for a milestone?

---

**Document Version:** 1.0
**Created:** 2025-11-13
**Last Updated:** 2025-11-13
