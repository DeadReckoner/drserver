# Next Milestone Generation — Frontend Integration Guide

This doc is for the mobile/frontend dev. It explains how to use
`POST /api/v1/agent/generate-next-milestone-async/:milestoneid` to generate
tasks for a milestone after the user has completed the prior one.

---

## Background

Roadmaps are generated in two phases:

1. **Strategic** — phases + milestones with metadata (sub_goal, description,
   acceptance test, duration, spikes, risks). **No tasks.**
2. **Tactical** — tasks + steps generated for the **first** milestone only.

Every subsequent milestone starts life with zero tasks. This endpoint fills
in that gap — AND, based on user feedback from the prior milestone, can
optionally refine the strategic fields of the milestone before generating
its tasks.

---

## When to call it

Call when the user completes a milestone and is ready to start the next one.
The UX is the 7-question review sheet; the user's answers are the payload.

- The target milestone must already exist in the DB (it was created during
  initial roadmap generation).
- Calling on a milestone that already has tasks will **wipe and regenerate**
  them (and may replace spikes/risks if the AI decides an adjustment is warranted).
- No concurrency lock yet — don't allow the user to kick off two jobs for the
  same milestone.

---

## Endpoint

```
POST /api/v1/agent/generate-next-milestone-async/:milestoneid
Authorization: Bearer <supabase JWT>
Content-Type: application/json
```

`:milestoneid` is a UUID. Invalid UUIDs return `400`.

### Request body

Embeds all `GoalRequest` fields (same shape you send to `/generate-async`)
plus a `review` object with answers to the 7 hardcoded UI questions.

```json
{
  "goalName": "Learn Full Stack Web Development",
  "goalDescription": "Build production-ready web apps end-to-end",
  "goalImportance": "Career advancement",
  "targetCompletionDate": "6 months",
  "weeklyTimeCommitment": "10 hours",
  "workFrequency": "Daily",
  "constraints": "Limited budget for courses",
  "currentSkillLevel": "Intermediate",
  "additionalContext": "Prefer hands-on projects",
  "review": {
    "completionStatus": "Mostly completed, skipped a few redundant tasks",
    "whatWorked": "Short daily tasks kept me consistent",
    "whatDidntWork": "Theory-heavy tasks felt disconnected from practice",
    "obstacles": "Work trip mid-week threw off the schedule",
    "paceFeedback": "A bit heavy — I'd prefer slightly fewer tasks per day",
    "clarityFeedback": "Steps 2-3 of 'setup environment' needed more detail",
    "goingForward": "More hands-on projects please; I can now do 15 hrs/week"
  }
}
```

Every `review` field is optional — empty strings are fine if the user skipped
a question. Passing an empty `review` object is valid (the AI will just
generate tasks without adjusting the milestone).

The 7 questions (exact wording from the mobile UI) map to the `review` fields:

| Field | Question |
|---|---|
| `completionStatus` | Q1: How did things go with this milestone? Did you complete everything or just check off some steps you didn't feel you needed? |
| `whatWorked` | Q2: What worked well? What parts felt smooth, effective, or motivating? |
| `whatDidntWork` | Q3: What didn't work? What parts felt confusing, difficult, unnecessary, or out of alignment with your situation? |
| `obstacles` | Q4: What got in your way? Any obstacles, practical or personal? |
| `paceFeedback` | Q5: How did the workload and pace feel? Did it fit your schedule and energy? |
| `clarityFeedback` | Q6: Were the instructions clear? Any parts that needed more explanation? |
| `goingForward` | Q7: What should change moving forward? Preferences for the next milestone? Anything in your life or priorities changed? |

### Response (202 Accepted)

```json
{
  "status": "pending",
  "job_id": "5a3f2e1d-...",
  "milestone_id": "b4c8e2f7-...",
  "roadmap_id": "9d1e7a3c-...",
  "message": "Your next milestone is being generated",
  "ws_url": "ws/agent/5a3f2e1d-..."
}
```

### Error responses

- `400` — malformed JSON or invalid milestone UUID
- `404` — milestone doesn't exist or isn't owned by the authenticated user
- `500` — server error creating the background job

AI / save failures during the job itself are surfaced over the websocket as
an `error` frame (see below), NOT as an HTTP error on this call.

---

## WebSocket — real-time progress

Same infrastructure as `/generate-async`. Connect to:

```
GET /api/v1/ws/agent/:jobid
Authorization: Bearer <supabase JWT>
```

You'll receive JSON frames matching the `ProgressUpdate` schema. The client
should send `{"type": "ping"}` periodically to keep the connection alive;
the server replies with `{"type": "pong"}`.

### Expected frame sequence (happy path)

1. **Initial status frame** (sent on connect, reflects current job state):
   ```json
   { "type": "status", "job_id": "...", "status": "processing", "progress": 5, "message": "..." }
   ```

2. **Progress frames**, in order:

   | % | Message |
   |---|---|
   | 5 | `Reviewing your milestone feedback...` |
   | 15 | `Analyzing what to adjust...` |
   | 45 | `Milestone reviewed — generating tasks...` (with `data`, see below) |
   | 80 | `Saving tasks...` |
   | 95 | `Finalizing...` |

   The 45% frame includes a `data` payload telling you whether the AI adjusted
   the milestone:
   ```json
   {
     "type": "progress",
     "job_id": "...",
     "status": "processing",
     "progress": 45,
     "message": "Milestone reviewed — generating tasks...",
     "data": {
       "adjusted": true,
       "rationale": "Increased duration by 2 days and clarified the acceptance test based on pace feedback."
     }
   }
   ```
   If `adjusted` is `false`, the milestone was left as-is.

3. **Completion frame:**
   ```json
   {
     "type": "complete",
     "job_id": "...",
     "status": "completed",
     "progress": 100,
     "message": "Your next milestone is ready!",
     "data": {
       "milestone_id": "...",
       "roadmap_id": "...",
       "adjusted": true
     }
   }
   ```

### Error frame

If anything fails (AI call, DB save, etc.):
```json
{
  "type": "error",
  "job_id": "...",
  "status": "failed",
  "message": "Failed to generate next milestone",
  "data": { "error": "<error message>" }
}
```

The roadmap's `generation_status` is **not** mutated by this flow — only the
job is marked `failed`. The existing roadmap stays intact.

---

## Polling fallback

If the websocket drops, poll:
```
GET /api/v1/jobs/:jobid/status
```

Returns the `BackgroundJob` shape. When `status === "completed"`, `result`
contains `{ milestone_id, roadmap_id, adjusted }`.

---

## Push notification

On success the server sends a push notification to the user's registered
Expo tokens:

- Type: `next_milestone_complete`
- Title: `Your next milestone is ready!`
- Body: `<milestone sub_goal> is ready to start.`
- Data: `{ milestone_id, roadmap_id }`

On failure:

- Type: `next_milestone_failed`
- Title: `Next Milestone Generation Failed`
- Body: `We encountered an issue. Please try again.`

---

## Suggested UI flow

1. User taps "Complete milestone & continue" on a finished milestone.
2. Show the 7-question review sheet. All answers optional.
3. On submit, POST the request. Show a progress UI driven by the websocket.
4. After receiving the 45% `progress` frame, if `data.adjusted === true`,
   consider surfacing `data.rationale` to the user ("We tweaked the plan
   based on your feedback: …") — this can run in parallel with the progress bar.
5. On `complete`, refetch the milestone (or the roadmap) — the newly-generated
   tasks + steps will be on it, and strategic fields may have changed.
6. On `error`, show the error and let the user retry with the same review.

---

## Things to know

- **Task regeneration is destructive.** If the user triggers this on a
  milestone that already has tasks, the old tasks (and their steps) are
  deleted. Steps delete via DB cascade.
- **Tasks come back pre-dated.** Unlike initial generation (where the user
  clicks "start roadmap" to populate dates), the new tasks are inserted with
  `date` fields already set — the roadmap is already running and we don't
  ask the user to "start" again. See the [Task dates](#task-dates) section
  below for the anchor logic.
- **The milestone may be re-dated.** `milestone.date` is updated to match the
  earliest task date, so the UI reflects when tactical work actually begins.
  `milestone.day` (the strategic planned-day offset) is NOT changed.
- **Spikes/Risks** may be replaced wholesale if the AI proposes new ones.
  Known limitation — tracked server-side via a TODO. For now, don't rely on
  client-side edits to spikes/risks persisting through this call.
- **Phase/roadmap statuses** are recomputed automatically after save, so the
  "current task" indicator and phase/roadmap status fields will update.
- **Milestone ordering** is not affected. The milestone keeps its `order_index`
  and `day`.
- **No concurrency guard yet.** Debounce in the UI — don't let the user fire
  two requests for the same milestone back-to-back.

### Task dates

Each new task's `date` is computed server-side as:

```
anchor = max(milestone.date, today)  // normalized to midnight UTC
task.date = anchor + (task.day - 1) days
```

`task.day` is milestone-relative (1–14 as generated by the AI), so the
task with `day = 1` lands on the anchor date and subsequent tasks fan out
from there.

Concretely:

- **If the user is on or ahead of schedule** (`milestone.date` is in the
  future): tasks follow the original strategic plan.
- **If the user is behind schedule** (`milestone.date` is in the past): tasks
  start today and fan out. Nothing lands in the past.
- **If `milestone.date` is null** (edge case — roadmap was never started via
  `/start`): tasks start today.

After inserting tasks, the server updates `milestone.date` to the earliest
task date so the milestone timeline is consistent with its tasks.

---

## Reference

- Handler: [api/handlers/agent.go](../api/handlers/agent.go) — `AgentGenerateNextMilestoneAsync`, `processNextMilestoneGeneration`
- AI orchestration: [agent/agent.go](../agent/agent.go) — `GenerateNextMilestoneFromReview`
- DB save: [database/roadmap.go](../database/roadmap.go) — `SaveNextMilestone`
- Route: [api/routes/routes.go](../api/routes/routes.go)
- Schemas: [openapi.yaml](../openapi.yaml) — `GenerateNextMilestoneRequest`, `MilestoneReviewAnswers`
