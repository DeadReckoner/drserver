# Roadmap Structure Migration Guide

## Overview

The roadmap data structure has been refactored to eliminate duplication and simplify the hierarchy. The separate `MacroMap` and `MacroMilestone` types have been merged into the existing `Milestone` type, creating a unified structure.

### Why This Change?

1. **Eliminated Duplication**: `MacroMilestone` and `Milestone` were essentially the same concept
2. **Proper Linking**: Milestones now belong to phases, making it easy to show which milestone is "active"
3. **Simplified Hierarchy**: One tree instead of two parallel hierarchies

---

## Structure Comparison

### OLD Structure
```
Roadmap
├── MacroMap (1:1 relationship)
│   └── Phases[]
│       └── MacroMilestones[]
│           ├── Spikes[]
│           └── Risks[]
└── Milestones[] (separate, unlinked to phases)
    └── Tasks[]
        └── Steps[]
```

### NEW Structure
```
Roadmap
├── Phases[]                    (AI-generated roadmaps use this)
│   └── Milestones[]
│       ├── Spikes[]
│       ├── Risks[]
│       └── Tasks[]
│           └── Steps[]
└── Milestones[]                (custom/manual roadmaps only)
```

**Note:** AI-generated roadmaps store milestones exclusively within the `Phases` structure. The top-level `Milestones[]` array is only used for manually-created roadmaps that don't use phases. This avoids duplicate milestone records in the database.

---

## Type Changes

### Roadmap

**Removed Fields:**
- `macro_map` - No longer exists

**Added Fields:**
- `phases` - Direct array of Phase objects

```typescript
// OLD
interface Roadmap {
  roadmap_id: string;
  user_id: string;
  goal: string;
  description: string;
  days: number;
  completed: boolean;
  start_date: string;
  type: "ai_generated" | "custom_made";
  anchor_id?: string;
  current_skill_level?: string;
  job_id?: string;
  generation_status: string;
  macro_map?: MacroMap;        // ❌ REMOVED
  milestones?: Milestone[];
}

// NEW
interface Roadmap {
  roadmap_id: string;
  user_id: string;
  goal: string;
  description: string;
  days: number;
  completed: boolean;
  start_date: string;
  type: "ai_generated" | "custom_made";
  anchor_id?: string;
  current_skill_level?: string;
  job_id?: string;
  generation_status: string;
  phases?: Phase[];            // ✅ NEW - Strategic structure
  milestones?: Milestone[];    // Still exists for tactical view
}
```

### Phase

**Changed Fields:**
- `macro_map_id` → `roadmap_id`
- `macro_milestones` → `milestones`

```typescript
// OLD
interface Phase {
  phase_id: string;
  macro_map_id: string;        // ❌ REMOVED
  name: string;
  description: string;
  order_index: number;
  duration_weeks: string;
  macro_milestones?: MacroMilestone[];  // ❌ REMOVED
}

// NEW
interface Phase {
  phase_id: string;
  roadmap_id: string;          // ✅ Direct link to roadmap
  name: string;
  description: string;
  order_index: number;
  duration_weeks: string;
  milestones?: Milestone[];    // ✅ Now contains Milestone objects
}
```

### Milestone (Enhanced)

The `Milestone` type now includes all strategic fields that were previously on `MacroMilestone`:

```typescript
// OLD
interface Milestone {
  milestone_id: string;
  roadmap_id: string;
  user_id: string;
  sub_goal: string;
  description: string;
  day: number;
  reached: boolean;
  date?: string;
  macro_milestone_id?: string;  // ❌ REMOVED
  tasks?: Task[];
}

// NEW
interface Milestone {
  milestone_id: string;
  roadmap_id: string;
  user_id: string;
  sub_goal: string;
  description: string;
  day: number;
  reached: boolean;
  date?: string;

  // ✅ NEW Strategic Fields (from MacroMilestone)
  phase_id?: string;
  acceptance_test?: string;
  duration_days_min?: number;
  duration_days_max?: number;
  order_index?: number;
  resources_forecast?: string;  // JSON string

  // ✅ NEW Related data (from MacroMilestone)
  spikes?: Spike[];
  risks?: Risk[];

  // Existing
  tasks?: Task[];
}
```

### Spike & Risk

**Changed Fields:**
- `macro_milestone_id` → `milestone_id`

```typescript
// OLD
interface Spike {
  spike_id: string;
  macro_milestone_id: string;  // ❌ REMOVED
  description: string;
  timebox_days: number;
}

// NEW
interface Spike {
  spike_id: string;
  milestone_id: string;        // ✅ Now references Milestone
  description: string;
  timebox_days: number;
}

// OLD
interface Risk {
  risk_id: string;
  macro_milestone_id: string;  // ❌ REMOVED
  description: string;
  severity: "low" | "medium" | "high";
}

// NEW
interface Risk {
  risk_id: string;
  milestone_id: string;        // ✅ Now references Milestone
  description: string;
  severity: "low" | "medium" | "high";
}
```

### MacroMap & MacroMilestone

**These types no longer exist. Remove them from your codebase.**

```typescript
// ❌ DELETED - Remove these types
interface MacroMap { ... }
interface MacroMilestone { ... }
```

---

## API Changes

### Removed Endpoints

```
❌ POST /api/v1/roadmaps/:roadmapid/macro-map
❌ GET  /api/v1/roadmaps/:roadmapid/macro-map
```

### Modified Endpoints

#### GET /api/v1/roadmaps/:roadmapid

The `view` query parameter has changed:

| Old Value | New Value | Description |
|-----------|-----------|-------------|
| `macro` | `strategic` | Phases with milestones (no tasks) |
| `milestones` | `milestones` | Milestones with tasks (unchanged) |
| `full` | `full` | Everything (unchanged, default) |

**Examples:**

```
GET /api/v1/roadmaps/{id}?view=strategic
GET /api/v1/roadmaps/{id}?view=milestones
GET /api/v1/roadmaps/{id}?view=full        (default)
```

#### POST /api/v1/agent/generateMilestone/:milestoneid

**Changed behavior**: Now generates tasks for an existing milestone instead of generating a new milestone.

**Request**: Same as before (GoalRequest body)

**Response**: Returns the milestone with newly generated tasks

```json
{
  "milestone_id": "...",
  "sub_goal": "MVP concept defined",
  "acceptance_test": "MVP scope documented",
  "tasks": [
    {
      "task_id": "...",
      "title": "Interview 5 freelancers",
      "steps": [...]
    }
  ]
}
```

---

## Response Format Changes

### Async Generation Response

The completion response no longer includes `macromap_id`:

```json
// OLD
{
  "type": "complete",
  "job_id": "...",
  "progress": 100,
  "status": "completed",
  "data": {
    "roadmap_id": "...",
    "macromap_id": "..."    // ❌ REMOVED
  }
}

// NEW
{
  "type": "complete",
  "job_id": "...",
  "progress": 100,
  "status": "completed",
  "data": {
    "roadmap_id": "..."     // Only roadmap_id now
  }
}
```

### Full Roadmap Response

```json
{
  "roadmap_id": "abc-123",
  "goal": "Launch a SaaS Product",
  "description": "Build and launch...",
  "days": 180,
  "completed": false,
  "type": "ai_generated",

  "phases": [
    {
      "phase_id": "phase-1",
      "roadmap_id": "abc-123",
      "name": "Phase 1: Foundations",
      "description": "Validate concept...",
      "order_index": 1,
      "duration_weeks": "8-12 weeks",
      "milestones": [
        {
          "milestone_id": "milestone-1",
          "roadmap_id": "abc-123",
          "user_id": "user-123",
          "phase_id": "phase-1",
          "sub_goal": "MVP concept defined",
          "description": "Define and validate MVP",
          "day": 14,
          "reached": false,
          "acceptance_test": "MVP scope documented and approved",
          "duration_days_min": 14,
          "duration_days_max": 28,
          "order_index": 1,
          "resources_forecast": "{\"books\": [\"Lean Startup\"]}",
          "spikes": [],
          "risks": [
            {
              "risk_id": "risk-1",
              "milestone_id": "milestone-1",
              "description": "Feature scope overreach",
              "severity": "high"
            }
          ],
          "tasks": [
            {
              "task_id": "task-1",
              "title": "Interview 5 freelancers",
              "description": "Conduct user interviews",
              "day": 1,
              "completed": false,
              "steps": [
                {
                  "step_id": "step-1",
                  "description": "Draft interview script",
                  "completed": false,
                  "number": 1
                }
              ]
            }
          ]
        }
      ]
    }
  ],

  "milestones": []  // Empty for AI-generated roadmaps (milestones are in phases)
}
```

---

## Frontend Migration Checklist

### 1. Update Type Definitions

- [ ] Remove `MacroMap` interface
- [ ] Remove `MacroMilestone` interface
- [ ] Update `Roadmap` interface (remove `macro_map`, add `phases`)
- [ ] Update `Phase` interface (change `macro_map_id` to `roadmap_id`, change `macro_milestones` to `milestones`)
- [ ] Update `Milestone` interface (add strategic fields)
- [ ] Update `Spike` interface (change `macro_milestone_id` to `milestone_id`)
- [ ] Update `Risk` interface (change `macro_milestone_id` to `milestone_id`)

### 2. Update API Calls

- [ ] Remove calls to `GET /roadmaps/:id/macro-map`
- [ ] Remove calls to `POST /roadmaps/:id/macro-map`
- [ ] Update `?view=macro` to `?view=strategic`
- [ ] Update handling of async generation completion (no `macromap_id`)

### 3. Update State Management (Redux)

- [ ] Update roadmap slice to handle new structure
- [ ] Remove any MacroMap-related state
- [ ] Update selectors for accessing phases/milestones
- [ ] Update any normalization logic

### 4. Update Components

#### Strategic View Components
- [ ] Update phase list to use `roadmap.phases` instead of `roadmap.macro_map?.phases`
- [ ] Update milestone rendering within phases to use `phase.milestones` instead of `phase.macro_milestones`
- [ ] Rename any `MacroMilestone` components to work with `Milestone`

#### Milestone Components
- [ ] Add display for new strategic fields:
  - `acceptance_test` - Completion criteria
  - `duration_days_min` / `duration_days_max` - Duration range
  - `resources_forecast` - Required resources (parse JSON)
  - `order_index` - Display order
- [ ] Update Spike/Risk components to use `milestone_id` instead of `macro_milestone_id`

#### Roadmap Generation Flow
- [ ] Update completion handler to not expect `macromap_id`
- [ ] Only first milestone will have tasks after initial generation
- [ ] Add UI to generate tasks for subsequent milestones using `POST /agent/generateMilestone/:id`

### 5. Update Tests

- [ ] Update mock data to use new structure
- [ ] Update any tests that reference MacroMap/MacroMilestone

---

## Key Behavioral Changes

### 1. Two-Stage Generation

AI generation now works in two stages:

**Stage 1: Strategic Roadmap** (10-50% progress)
- Generates all Phases
- Generates all Milestones with strategic metadata (acceptance tests, durations, spikes, risks)
- NO tasks generated yet

**Stage 2: First Milestone Tasks** (50-80% progress)
- Generates Tasks and Steps for the FIRST milestone only
- Other milestones have empty `tasks` arrays

### 2. Progressive Task Generation

Users can generate tasks for additional milestones on-demand:

```typescript
// Generate tasks for a specific milestone
const response = await fetch(`/api/v1/agent/generateMilestone/${milestoneId}`, {
  method: 'POST',
  body: JSON.stringify(goalRequest)
});

const milestoneWithTasks = await response.json();
```

### 3. Milestone Location

Milestone location depends on roadmap type:

**AI-Generated Roadmaps:**
- Milestones are stored ONLY in phases: `roadmap.phases[].milestones[]`
- The top-level `roadmap.milestones[]` array will be empty
- This prevents duplicate records in the database

**Custom/Manual Roadmaps:**
- Milestones are stored in the top-level array: `roadmap.milestones[]`
- The `phases` array will be empty (phases are not used for manual roadmaps)

---

## Example: Accessing Data

### Old Way (Remove This)
```typescript
// ❌ OLD - No longer works
const phases = roadmap.macro_map?.phases || [];
const milestones = phases.flatMap(p => p.macro_milestones || []);
const risks = milestone.risks; // was on MacroMilestone
```

### New Way
```typescript
// ✅ NEW - Use this

// For AI-generated roadmaps: milestones are in phases
const phases = roadmap.phases || [];
const milestonesFromPhases = phases.flatMap(p => p.milestones || []);

// For custom/manual roadmaps: milestones are in top-level array
const manualMilestones = roadmap.milestones || [];

// Universal helper: get milestones regardless of roadmap type
const getAllMilestones = (roadmap: Roadmap) => {
  if (roadmap.phases && roadmap.phases.length > 0) {
    // AI-generated: get from phases
    return roadmap.phases.flatMap(p => p.milestones || []);
  }
  // Custom/manual: use top-level array
  return roadmap.milestones || [];
};

// Strategic fields now on Milestone
const acceptanceTest = milestone.acceptance_test;
const durationRange = `${milestone.duration_days_min}-${milestone.duration_days_max} days`;
const risks = milestone.risks || [];
const spikes = milestone.spikes || [];

// Parse resources forecast
const resources = milestone.resources_forecast
  ? JSON.parse(milestone.resources_forecast)
  : { books: [], tools: [], courses: [], budget: "$0" };
```

---

## Questions?

Contact the backend team if you have questions about the new structure or need clarification on any changes.
