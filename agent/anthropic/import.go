package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"dr/server/log"
	"dr/server/types"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

const (
	// importModel is the model used to transpose an uploaded plan. Kept
	// separate from the structured-generation calls in provider.go: reading a
	// document the user already wrote and preserving its structure is a harder
	// job than generating from a form, so this path pays for the larger model.
	importModel = "claude-opus-5"

	// importMaxTokens covers a full multi-phase plan with tasks and steps.
	// Above ~16k the SDK's HTTP timeout starts to bite on non-streaming
	// requests, which is why this call streams and accumulates.
	importMaxTokens = 32000

	importToolName = "transpose_plan"

	// defaultMilestoneDurationDays is the fallback window for a milestone the
	// source document gave no duration for.
	defaultMilestoneDurationDays = 14
)

// ErrPlanRefused is returned when the model declines to transpose the uploaded
// document (stop_reason "refusal"). Distinguished from a transport failure so
// the caller can tell the user their document was rejected rather than
// reporting a server error and inviting an identical retry.
var ErrPlanRefused = errors.New("the uploaded document was declined by the model")

// ErrPlanTooLarge is returned when the model ran out of output tokens before
// finishing the transposition, which leaves the tool payload truncated.
var ErrPlanTooLarge = errors.New("the plan was too large to transpose in one pass")

// PlanImport is the result of transposing an uploaded plan document onto the
// Dead Reckoner roadmap model.
type PlanImport struct {
	// Roadmap is normalized and ready to persist, but carries no IDs — the
	// caller assigns RoadmapID/UserID before saving.
	Roadmap types.Roadmap `json:"roadmap"`
	// Notes is the model's account of what it preserved versus inferred.
	Notes types.PlanImportNotes `json:"import_notes"`
}

// ImportPlanFromPDF sends an uploaded plan PDF to Claude and returns it
// transposed onto Roadmap → Phase → Milestone → Task → Step.
//
// The prompt optimizes for fidelity: the document is the source of truth and
// the model is told to preserve the author's structure and wording, inventing
// only the fields the roadmap model requires (day offsets, durations,
// acceptance tests) and recording each of those inventions in Notes.Inferred.
//
// now anchors any calendar dates in the document to day offsets; callers pass
// time.Now().
func (p *Provider) ImportPlanFromPDF(ctx context.Context, pdf []byte, hints types.PlanImportContext, now time.Time) (*PlanImport, error) {
	log.Info(ctx, "Starting Anthropic plan import", fmt.Sprintf("pdf_bytes=%d", len(pdf)))

	tool := transposePlanTool()
	params := anthropic.MessageNewParams{
		Model:     importModel,
		MaxTokens: importMaxTokens,
		System: []anthropic.TextBlockParam{
			{Text: transposePlanSystemPrompt()},
		},
		Messages: []anthropic.MessageParam{
			// Document block before the text block: Claude reads a document
			// more reliably when it precedes the instructions about it.
			anthropic.NewUserMessage(
				anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
					Data: base64.StdEncoding.EncodeToString(pdf),
				}),
				anthropic.NewTextBlock(buildImportPrompt(hints, now)),
			),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: importToolName,
			},
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffortHigh,
		},
	}

	stream := p.client.Messages.NewStreaming(ctx, params)
	msg := anthropic.Message{}
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("anthropic stream error: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return nil, ErrPlanRefused
	case anthropic.StopReasonMaxTokens:
		return nil, ErrPlanTooLarge
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == importToolName {
			result, err := parsePlanImport(block.Input)
			if err != nil {
				return nil, err
			}
			log.Info(ctx, "Plan import transposed", fmt.Sprintf(
				"phases=%d, fidelity=%s", len(result.Roadmap.Phases), result.Notes.Fidelity))
			return result, nil
		}
	}

	return nil, fmt.Errorf("no tool use found in response (stop_reason=%s)", msg.StopReason)
}

// parsePlanImport turns the transpose_plan tool payload into a persistable
// roadmap, normalizing the parts the model is free to get wrong.
func parsePlanImport(input []byte) (*PlanImport, error) {
	normalized, err := normalizeDependencyArrays(input)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize plan payload: %w", err)
	}

	var result PlanImport
	if err := json.Unmarshal(normalized, &result); err != nil {
		return nil, fmt.Errorf("failed to parse plan from tool output: %w", err)
	}

	if err := normalizeImportedRoadmap(&result.Roadmap); err != nil {
		return nil, err
	}
	return &result, nil
}

// normalizeDependencyArrays rewrites every "dependencies" value that arrived as
// a JSON array into a JSON-array string literal, so it unmarshals into
// types.Task.Dependencies (*string). Same quirk normalizeTaskDependencies
// handles for the generation path — the model reliably prefers a real array
// even when the schema types the field as a string — but applied to the whole
// nested payload in one pass rather than task by task.
func normalizeDependencyArrays(input []byte) ([]byte, error) {
	var root any
	if err := json.Unmarshal(input, &root); err != nil {
		return nil, err
	}
	rewritten, err := rewriteDependencies(root)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rewritten)
}

func rewriteDependencies(node any) (any, error) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "dependencies" {
				if arr, ok := child.([]any); ok {
					encoded, err := json.Marshal(arr)
					if err != nil {
						return nil, err
					}
					v[key] = string(encoded)
					continue
				}
			}
			rewrittenChild, err := rewriteDependencies(child)
			if err != nil {
				return nil, err
			}
			v[key] = rewrittenChild
		}
		return v, nil
	case []any:
		for i := range v {
			rewrittenItem, err := rewriteDependencies(v[i])
			if err != nil {
				return nil, err
			}
			v[i] = rewrittenItem
		}
		return v, nil
	default:
		return node, nil
	}
}

// normalizeImportedRoadmap makes a model-produced roadmap safe to persist and
// to schedule.
//
// The scheduling contract matters most: RoadmapHandler.StartRoadmap dates both
// milestones and tasks as startDate + Day, so Day is a roadmap-global offset,
// not a per-milestone one. The model is told this, but a plan with vague or
// missing timing produces day numbers that repeat or run backwards, which would
// scramble the ordering (milestones sort by day) and the computed statuses.
// Here days are forced into a non-decreasing sequence, tasks are pinned inside
// their milestone's window, and order indexes are reassigned from the order the
// model returned things in — which is the order the source document had them.
//
// Content is never dropped: empty titles get positional fallbacks rather than
// being discarded, because the whole point of an import is to keep the user's
// plan intact.
func normalizeImportedRoadmap(r *types.Roadmap) error {
	r.Goal = strings.TrimSpace(r.Goal)
	if r.Goal == "" {
		r.Goal = "Imported plan"
	}
	r.Description = strings.TrimSpace(r.Description)
	r.Completed = false

	// Collect milestones in document order: phase milestones first (phases in
	// order), then any the model emitted at the top level. The day cursor runs
	// across all of them because Day is roadmap-global.
	var milestones []*types.Milestone
	for i := range r.Phases {
		phase := &r.Phases[i]
		phase.OrderIndex = i
		phase.Name = strings.TrimSpace(phase.Name)
		if phase.Name == "" {
			phase.Name = fmt.Sprintf("Phase %d", i+1)
		}
		phase.Description = strings.TrimSpace(phase.Description)
		for j := range phase.Milestones {
			milestones = append(milestones, &phase.Milestones[j])
		}
	}
	for i := range r.Milestones {
		milestones = append(milestones, &r.Milestones[i])
	}

	if len(milestones) == 0 {
		return errors.New("no milestones could be read from the document")
	}

	dayCursor := 0
	for i, m := range milestones {
		m.OrderIndex = i
		m.Reached = false
		m.Date = nil
		m.SubGoal = strings.TrimSpace(m.SubGoal)
		if m.SubGoal == "" {
			m.SubGoal = fmt.Sprintf("Milestone %d", i+1)
		}
		m.Description = strings.TrimSpace(m.Description)
		m.AcceptanceTest = strings.TrimSpace(m.AcceptanceTest)

		duration := milestoneDuration(m)
		if m.DurationDaysMin <= 0 {
			m.DurationDaysMin = duration
		}
		if m.DurationDaysMax < m.DurationDaysMin {
			m.DurationDaysMax = m.DurationDaysMin
		}

		if m.Day < dayCursor {
			m.Day = dayCursor
		}
		normalizeMilestoneTasks(m, duration)

		dayCursor = m.Day + duration
	}

	if r.Days < dayCursor {
		r.Days = dayCursor
	}
	return nil
}

// milestoneDuration picks the milestone's working window in days, preferring
// the document's upper bound so a user tracking to the pessimistic end of a
// stated range isn't overdue the moment the roadmap starts. Falls back to the
// lower bound, then to a fortnight.
func milestoneDuration(m *types.Milestone) int {
	switch {
	case m.DurationDaysMax > 0:
		return m.DurationDaysMax
	case m.DurationDaysMin > 0:
		return m.DurationDaysMin
	default:
		return defaultMilestoneDurationDays
	}
}

// normalizeMilestoneTasks fixes up a milestone's tasks in place: content
// fields, step numbering, and day placement inside the milestone's window.
//
// Day placement has two modes. When the document carried no usable schedule —
// every task on the same day, which is what a plan with no timing produces —
// tasks are spread one per day so the user isn't handed the whole milestone on
// its first day. When the model did schedule them, those days are kept and only
// forced non-decreasing and clamped into the window, since two tasks sharing a
// day can be deliberate.
func normalizeMilestoneTasks(m *types.Milestone, duration int) {
	windowEnd := m.Day + duration - 1
	scheduled := tasksAreScheduled(m.Tasks)

	lastDay := m.Day
	for i := range m.Tasks {
		task := &m.Tasks[i]
		task.Completed = false
		task.Status = ""
		task.Date = nil
		task.Title = strings.TrimSpace(task.Title)
		if task.Title == "" {
			task.Title = fmt.Sprintf("Task %d", i+1)
		}
		task.Description = strings.TrimSpace(task.Description)

		if scheduled {
			if task.Day < lastDay {
				task.Day = lastDay
			}
			if task.Day > windowEnd {
				task.Day = windowEnd
			}
		} else {
			task.Day = min(m.Day+i, windowEnd)
		}
		lastDay = task.Day

		normalizeTaskSteps(task)
	}
}

// tasksAreScheduled reports whether the tasks carry a real schedule. All-equal
// days (typically all zero) mean the document had no timing, which is
// indistinguishable from "every task happens on the milestone's first day".
func tasksAreScheduled(tasks []types.Task) bool {
	for i := range tasks {
		if tasks[i].Day != tasks[0].Day {
			return true
		}
	}
	return false
}

func normalizeTaskSteps(task *types.Task) {
	steps := make([]types.Step, 0, len(task.Steps))
	for _, step := range task.Steps {
		step.Description = strings.TrimSpace(step.Description)
		if step.Description == "" {
			continue
		}
		step.Completed = false
		step.Status = string(types.StepStatusIncomplete)
		step.Number = len(steps) + 1
		steps = append(steps, step)
	}
	task.Steps = steps
}

// ─── Prompts ──────────────────────────────────────────────────────────────────

func transposePlanSystemPrompt() string {
	return `You transpose a plan the user has ALREADY WRITTEN onto the Dead Reckoner roadmap structure. You are not a planning coach here: the document is the source of truth and your job is to move it across intact, not to improve it.

STRUCTURE YOU ARE MAPPING ONTO:
Roadmap → Phases → Milestones → Tasks → Steps

- Phase: a major stage of the plan. Use the document's own top-level grouping (parts, stages, quarters, tracks, chapters). If the document has no grouping, put every milestone in a single phase named after the goal.
- Milestone: a concrete outcome or deliverable the user reaches. Use the document's headings, deliverables, checkpoints, or numbered goals.
- Task: an action the user performs. Use the document's action items, bullets, or to-dos.
- Step: a sub-instruction inside a task. Use the document's sub-bullets or checklists.

PRESERVATION RULES (these outrank everything else):
1. Keep the author's own wording for titles and descriptions. Lightly trim for length; do not rewrite, re-scope, or embellish.
2. Keep the document's ordering, numbering, and terminology, including jargon and project-specific names.
3. Keep every action item the document contains, at whatever depth it appears. Never drop a milestone or task because it seems redundant, badly scoped, or unrealistic — that is the user's plan and they will edit it themselves.
4. Keep concrete details the document supplies: durations, dates, effort estimates, dependencies, resources, risks, owners (fold an owner into the description).
5. Do not add phases, milestones, or tasks that represent work the author did not plan.

FILLING GAPS (only where the structure requires a value):
The roadmap model needs fields most documents will not have. Supply these, conservatively, and list each kind of gap you filled in import_notes.inferred:
- acceptance_test: how the user knows the milestone is done. Derive it from the milestone's own description.
- duration_days_min / duration_days_max: from the document's timing if stated; otherwise estimate from the amount of work described.
- day: see DAY NUMBERING below.
- Tasks: if a milestone in the document has no action items, generate 8-12 tasks ONLY for the FIRST milestone, so the user has somewhere to start. Leave tasks empty for later milestones with no source content — the user generates those later, one milestone at a time.
- Steps: only add steps when the document's task is too vague to act on. A task carried over intact needs no invented steps.

DAY NUMBERING:
"day" is a whole-number offset from the day the user starts the roadmap. Day 0 is the start day. It is GLOBAL across the roadmap, not relative to the phase or milestone.
- Milestone days must never decrease as you go through the plan in order.
- Each task's day must fall inside its own milestone's window: at or after the milestone's day, and before the milestone's day plus its duration.
- If the document has real calendar dates, convert them to offsets from the earliest date in the plan.
- If the document has no timing at all, sequence the milestones back-to-back using your duration estimates.

OTHER RULES:
- dependencies is a JSON array of the exact titles of prerequisite tasks in the same milestone, or [] when there are none.
- Set every "completed" field to false: an import starts fresh, even if the document shows finished work. Mention already-completed work in the milestone description instead.
- Use empty arrays, never null, for spikes, risks, tasks, and steps.
- Content that does not belong in a roadmap (budgets, org charts, meeting notes, appendices, reference material) goes in import_notes.dropped. Do not force it into a milestone.
- Judge fidelity honestly: "high" when the roadmap is essentially the document restructured, "medium" when you supplied a fair amount of timing or task detail, "low" when the document was thin enough that most of the roadmap is your inference.
- If the document is not a plan at all, still return your best reading of it and say so plainly in import_notes.source_summary.

You MUST call the transpose_plan tool with your response.`
}

// buildImportPrompt is the user turn accompanying the PDF. The hints are
// optional context the client collected; they only resolve what the document
// leaves open, and must never override it.
func buildImportPrompt(hints types.PlanImportContext, now time.Time) string {
	var b strings.Builder
	b.WriteString("Transpose the attached plan onto the roadmap structure, preserving as much of it as you can.\n\n")
	b.WriteString(fmt.Sprintf("Today's date is %s (use it to convert any calendar dates in the document into day offsets).\n", now.Format("2006-01-02")))

	var provided []string
	appendIf := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		provided = append(provided, fmt.Sprintf("- %s: %s", label, strings.TrimSpace(value)))
	}
	appendIf("Goal name", hints.GoalName)
	appendIf("Target completion date", hints.TargetCompletionDate)
	appendIf("Weekly time commitment", hints.WeeklyTimeCommitment)
	appendIf("Work frequency", hints.WorkFrequency)
	appendIf("Current skill level", hints.CurrentSkillLevel)
	appendIf("Constraints", hints.Constraints)
	appendIf("Additional context", hints.AdditionalContext)

	if len(provided) == 0 {
		b.WriteString("\nThe user supplied no extra context, so rely entirely on the document.")
		return b.String()
	}

	b.WriteString("\nThe user also supplied this context. Use it ONLY to fill gaps the document leaves open (pacing, timing, unstated goal). Where it conflicts with the document, the document wins.\n")
	b.WriteString(strings.Join(provided, "\n"))
	return b.String()
}

// ─── Tool Definition ──────────────────────────────────────────────────────────

func strProp(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func intProp(description string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": description}
}

func transposePlanTool() anthropic.ToolParam {
	stepSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"description": strProp("A single sub-instruction, in the document's own words where it has one"),
			"number":      intProp("1-based position of this step within the task"),
			"completed":   map[string]interface{}{"type": "boolean", "description": "Always false"},
		},
		"required": []string{"description", "number", "completed"},
	}

	taskSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":        strProp("Short action title, no more than 5 words, from the document where possible"),
			"description":  strProp("What the task involves, preserving the document's detail"),
			"rationale":    strProp("Why this task matters, if the document says; otherwise a one-line derivation from it"),
			"est_minutes":  intProp("Estimated minutes, from the document if stated"),
			"dependencies": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Exact titles of prerequisite tasks in the same milestone, or []"},
			"confidence":   map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}, "description": "Confidence in the time estimate"},
			"day":          intProp("Global day offset from roadmap start; must sit inside the parent milestone's window"),
			"completed":    map[string]interface{}{"type": "boolean", "description": "Always false"},
			"steps": map[string]interface{}{
				"type":        "array",
				"items":       stepSchema,
				"description": "Sub-steps from the document; empty array when the task needs none",
			},
		},
		"required": []string{"title", "description", "day", "completed", "steps"},
	}

	milestoneSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"sub_goal":           strProp("Milestone name, no more than 5 words, from the document's heading where possible"),
			"description":        strProp("What this milestone covers, preserving the document's detail"),
			"acceptance_test":    strProp("How the user knows this milestone is complete"),
			"duration_days_min":  intProp("Minimum days to reach this milestone"),
			"duration_days_max":  intProp("Maximum days to reach this milestone"),
			"order_index":        intProp("0-based position of this milestone within its phase"),
			"day":                intProp("Global day offset from roadmap start when this milestone begins; never decreases through the plan"),
			"resources_forecast": strProp(`Resources named by the document, as a JSON string like {"books":[],"tools":[],"courses":[],"budget":""}`),
			"spikes": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description":  strProp("An open question the document flags as needing investigation"),
						"timebox_days": intProp("Days to timebox the investigation"),
					},
					"required": []string{"description", "timebox_days"},
				},
				"description": "Only unknowns the document itself raises; empty array otherwise",
			},
			"risks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"description": strProp("A risk the document names"),
						"severity":    map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}},
					},
					"required": []string{"description", "severity"},
				},
				"description": "Only risks the document itself names; empty array otherwise",
			},
			"tasks": map[string]interface{}{
				"type":        "array",
				"items":       taskSchema,
				"description": "Action items from the document. Empty array for later milestones the document gives no actions for",
			},
		},
		"required": []string{"sub_goal", "description", "acceptance_test", "duration_days_min", "duration_days_max", "order_index", "day", "spikes", "risks", "tasks"},
	}

	phaseSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":           strProp("Phase name, from the document's own grouping"),
			"description":    strProp("What this phase of the plan covers"),
			"order_index":    intProp("0-based position of this phase in the roadmap"),
			"duration_weeks": strProp(`Approximate span in weeks, e.g. "4-6"`),
			"milestones": map[string]interface{}{
				"type":  "array",
				"items": milestoneSchema,
			},
		},
		"required": []string{"name", "description", "order_index", "milestones"},
	}

	return anthropic.ToolParam{
		Name:        importToolName,
		Description: param.NewOpt("Transpose an existing plan document onto the Dead Reckoner roadmap structure, preserving the author's content"),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"roadmap": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"goal":                strProp("The overall goal, taken from the document's title or stated objective"),
						"description":         strProp("A short description of the plan, in the document's own framing"),
						"days":                intProp("Total days the plan spans from start to finish"),
						"current_skill_level": strProp("The starting point the document describes, if any"),
						"phases": map[string]interface{}{
							"type":  "array",
							"items": phaseSchema,
						},
					},
					"required": []string{"goal", "description", "days", "phases"},
				},
				"import_notes": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"source_summary": strProp("One or two sentences on what the document is"),
						"fidelity":       map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}, "description": "How much of the roadmap came from the document rather than inference"},
						"inferred": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Each gap you filled that the document did not supply",
						},
						"dropped": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Document content that did not map onto the roadmap structure",
						},
					},
					"required": []string{"source_summary", "fidelity", "inferred", "dropped"},
				},
			},
			Required: []string{"roadmap", "import_notes"},
		},
	}
}
