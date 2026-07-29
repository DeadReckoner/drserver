# Plan Import — Frontend Integration Guide

This doc is for the mobile/frontend dev. It explains how to use
`POST /api/v1/agent/import-plan` to let a user upload a plan or roadmap they
already have (as a PDF) and get it transposed onto the Dead Reckoner roadmap
structure.

---

## Background

Roadmap generation asks the user a form of questions and invents a plan from
scratch. Plan import is the other direction: the user already has a plan — a
course syllabus, a training program, a project plan, a coach's PDF — and wants
it inside the app.

The server sends the PDF to Claude with instructions to **preserve** the
document rather than improve it, and to fill in only the fields the roadmap
model requires (day offsets, durations, acceptance tests). Every gap the model
fills is reported back in `import_notes.inferred`, so the UI can be honest with
the user about what came from their document and what didn't.

The result is a normal roadmap. Everything downstream — `/start`, milestone
generation, statuses, anchors — works on it unchanged.

---

## Endpoint

```
POST /api/v1/agent/import-plan
Authorization: Bearer <supabase JWT>
Content-Type: multipart/form-data
```

### Request

| Field     | Required | Description                                                        |
| --------- | -------- | ------------------------------------------------------------------ |
| `file`    | yes      | The PDF. Max 10 MB. Validated by magic number, not by filename.    |
| `context` | no       | JSON string of optional hints (below).                             |

`context` fields (all optional, all strings):

```json
{
  "goalName": "Run a sub-4 marathon",
  "targetCompletionDate": "6 months",
  "weeklyTimeCommitment": "8 hours",
  "workFrequency": "Daily",
  "currentSkillLevel": "Ran one half marathon",
  "constraints": "Travel two weeks in March",
  "additionalContext": "Coming back from a knee injury"
}
```

These only resolve what the document leaves open (pacing, timing, an unstated
goal). Where a hint conflicts with the document, **the document wins** — the
model is told so explicitly. Sending no `context` at all is fine and is the
expected path for a self-contained plan.

### Response — `202 Accepted`

```json
{
  "status": "pending",
  "job_id": "6f1c...",
  "roadmap_id": "9ab2...",
  "message": "Your plan is being imported",
  "ws_url": "ws/agent/6f1c..."
}
```

The `roadmap_id` is a draft with `generation_status: "processing"`. Navigate to
it immediately; it fills in when the job completes.

### Error responses

| Status | When                                                                  |
| ------ | --------------------------------------------------------------------- |
| `400`  | No file, the file isn't a PDF, or `context` isn't valid JSON          |
| `403`  | AI acknowledgment not accepted (`code: "ai_ack_required"`)            |
| `402`  | Daily AI generation cap reached (`code: "plan_limit"`) — same paywall trigger as the generation endpoints |
| `413`  | PDF is over 10 MB                                                     |
| `503`  | `ANTHROPIC_API_KEY` not configured on the server                      |

The daily AI cap is the same pool the generation endpoints draw from, and — as
with those — it is only **consumed on success**. A failed import doesn't cost
the user a generation.

---

## Progress

Identical to roadmap generation: connect to `GET /ws/agent/:jobid`, or poll
`GET /api/v1/jobs/:jobid/status`.

Progress messages: `Reading your plan...` → `Transposing your plan onto a
roadmap...` → `Saving your roadmap...` → `Finalizing...`

### Completion payload

The `complete` websocket frame and the job's `result` both carry the import
notes alongside the roadmap ID:

```json
{
  "roadmap_id": "9ab2...",
  "import_notes": {
    "source_summary": "A 12-week half-marathon training plan with weekly mileage targets.",
    "fidelity": "high",
    "inferred": [
      "The plan had no explicit completion criteria; derived an acceptance test per week block"
    ],
    "dropped": ["Nutrition reference tables in the appendix"]
  }
}
```

**Show this to the user on the review screen.** `fidelity` is `high`, `medium`,
or `low`; `inferred` is what the model added; `dropped` is source content that
didn't map onto a roadmap (budgets, reference material, meeting notes). A `low`
fidelity result is a signal to prompt the user to review carefully before
starting.

### Failure

On failure the job goes to `failed` and the websocket sends an `error` frame
whose `message` is already user-facing copy:

- Declined document → "We couldn't import that document. Try uploading the plan
  itself, without unrelated material."
- Too long to transpose → "That plan was too long to import in one go. Try
  splitting it into smaller documents."
- Anything else → "We encountered an issue importing your plan. Please try
  again."

The draft roadmap is left with `generation_status: "failed"`, same as a failed
generation.

---

## What the import produces

The document is mapped onto the existing hierarchy:

| Document                              | Roadmap   |
| ------------------------------------- | --------- |
| Top-level grouping (parts, quarters)  | Phase     |
| Deliverables, checkpoints, headings   | Milestone |
| Action items, to-dos, bullets         | Task      |
| Sub-bullets, checklists               | Step      |

A flat document with no grouping becomes a single phase.

Notes for the client:

- **Tasks are not generated for every milestone.** Tasks the document contains
  are carried over wherever they appear. Where the document has no action items,
  only the **first** milestone gets generated tasks — the rest are filled in
  later via `/agent/generate-next-milestone-async/:milestoneid`, same as an
  AI-generated roadmap.
- **Everything starts incomplete.** Even if the source document shows finished
  work, all `completed` flags come back `false`; completed work is mentioned in
  the milestone description instead.
- **Days are offsets, not dates.** As with generated roadmaps, milestone and
  task `day` values are offsets from the start date and only become real dates
  when the user calls `/roadmaps/:roadmapid/start`.
- **Type is `ai_generated`.** Imported roadmaps carry the same type as generated
  ones; there's no separate import type.

---

## Server implementation notes

- `api/handlers/agent_import.go` — endpoint, validation, background job.
- `agent/anthropic/import.go` — the Claude call (`transpose_plan` tool), the
  prompt, and the normalization that makes model output safe to schedule.
- The PDF is **not stored**. It is read into memory, sent to the model, and
  dropped; the job payload records only the filename and size.
- This path is Anthropic-only regardless of `AI_PROVIDER` — it needs PDF input,
  which the OpenAI path doesn't implement.
