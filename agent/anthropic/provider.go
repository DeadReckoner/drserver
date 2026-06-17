package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"dr/server/log"
	"dr/server/types"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// Provider implements the agent.Provider interface using Anthropic's API
type Provider struct {
	client anthropic.Client
}

// NewProvider creates a new Anthropic provider
func NewProvider() *Provider {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &Provider{client: client}
}

func (p *Provider) Name() string { return "anthropic" }

// GenerateStrategicRoadmap generates the high-level roadmap with phases and milestones
func (p *Provider) GenerateStrategicRoadmap(ctx context.Context, goalReq types.GoalRequest) (*types.Roadmap, error) {
	log.Info(ctx, "Starting Anthropic strategic roadmap generation")

	userPrompt := buildGoalPrompt(goalReq)
	systemPrompt := strategicRoadmapSystemPrompt()
	tool := strategicRoadmapTool()

	// Call Anthropic with tool use
	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: "generate_strategic_roadmap",
			},
		},
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	// Extract tool use result
	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == "generate_strategic_roadmap" {
			var roadmap types.Roadmap
			if err := json.Unmarshal(block.Input, &roadmap); err != nil {
				return nil, fmt.Errorf("failed to parse roadmap from tool output: %w", err)
			}
			log.Info(ctx, "Strategic roadmap generated", fmt.Sprintf("Phases: %d", len(roadmap.Phases)))
			return &roadmap, nil
		}
	}

	return nil, fmt.Errorf("no tool use found in response")
}

// GenerateMilestoneTasks generates tasks for a specific milestone
func (p *Provider) GenerateMilestoneTasks(ctx context.Context, goalReq types.GoalRequest, milestone *types.Milestone) ([]types.Task, error) {
	log.Info(ctx, "Starting Anthropic milestone tasks generation", milestone.MilestoneID)

	userPrompt := buildGoalPrompt(goalReq)
	milestoneContext := buildMilestoneContext(milestone)
	systemPrompt := milestoneTasksSystemPrompt(milestoneContext, "")
	tool := milestoneTasksTool()

	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: "generate_milestone_tasks",
			},
		},
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	// Extract tool use result
	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == "generate_milestone_tasks" {
			tasks, err := parseMilestoneTasks(block.Input)
			if err != nil {
				return nil, fmt.Errorf("failed to parse tasks from tool output: %w", err)
			}
			log.Info(ctx, "Milestone tasks generated", fmt.Sprintf("Tasks: %d", len(tasks)))
			return tasks, nil
		}
	}

	return nil, fmt.Errorf("no tool use found in response")
}

// GenerateNextMilestoneFromReview generates the next milestone based on user review
func (p *Provider) GenerateNextMilestoneFromReview(
	ctx context.Context,
	goalReq types.GoalRequest,
	milestone *types.Milestone,
	review types.MilestoneReviewAnswers,
) (*types.MilestoneAdjustments, []types.Task, error) {
	log.Info(ctx, "Starting Anthropic next milestone generation",
		fmt.Sprintf("Goal: %s, Milestone: %s", goalReq.GoalName, milestone.SubGoal))

	userPrompt := buildGoalPrompt(goalReq)
	milestoneContext := buildMilestoneContext(milestone)
	reviewContext := buildReviewContext(review)

	// STEP 1: Generate adjustments
	adjustments, err := p.generateAdjustments(ctx, userPrompt, milestoneContext, reviewContext)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate adjustments: %w", err)
	}

	// Apply adjustments to milestone for task generation
	adjusted := *milestone
	if adjustments.Adjusted {
		applyAdjustments(&adjusted, adjustments)
	}

	// STEP 2: Generate tasks with review context
	adjustedContext := buildMilestoneContext(&adjusted)
	systemPrompt := milestoneTasksSystemPrompt(adjustedContext, reviewContext)
	tool := milestoneTasksTool()

	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 8192,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: "generate_milestone_tasks",
			},
		},
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic API error: %w", err)
	}

	// Extract tasks
	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == "generate_milestone_tasks" {
			tasks, err := parseMilestoneTasks(block.Input)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse tasks: %w", err)
			}
			log.Info(ctx, "Next milestone tasks generated", fmt.Sprintf("Tasks: %d", len(tasks)))
			return adjustments, tasks, nil
		}
	}

	return nil, nil, fmt.Errorf("no tool use found in response")
}

func (p *Provider) generateAdjustments(ctx context.Context, userPrompt, milestoneContext, reviewContext string) (*types.MilestoneAdjustments, error) {
	systemPrompt := adjustmentsSystemPrompt(milestoneContext, reviewContext)
	tool := adjustmentsTool()

	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice: anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: "generate_milestone_adjustments",
			},
		},
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic API error: %w", err)
	}

	for _, block := range msg.Content {
		if block.Type == "tool_use" && block.Name == "generate_milestone_adjustments" {
			var adj types.MilestoneAdjustments
			if err := json.Unmarshal(block.Input, &adj); err != nil {
				return nil, fmt.Errorf("failed to parse adjustments: %w", err)
			}
			return &adj, nil
		}
	}

	return nil, fmt.Errorf("no tool use found in response")
}

// ─── Helper Functions ─────────────────────────────────────────────────────────

// parseMilestoneTasks extracts the task list from a generate_milestone_tasks
// tool call. The tool schema types "dependencies" as a string, but the prompt
// asks for a JSON array of task titles, so Claude often returns an actual array.
// types.Task.Dependencies is a *string holding a JSON-array literal (matching the
// OpenAI path), so any array form is normalized to that string before unmarshaling.
func parseMilestoneTasks(input []byte) ([]types.Task, error) {
	var wrapper struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(input, &wrapper); err != nil {
		return nil, err
	}

	tasks := make([]types.Task, 0, len(wrapper.Tasks))
	for _, rawTask := range wrapper.Tasks {
		normalized, err := normalizeTaskDependencies(rawTask)
		if err != nil {
			return nil, err
		}
		var task types.Task
		if err := json.Unmarshal(normalized, &task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// normalizeTaskDependencies rewrites a task's "dependencies" field from a JSON
// array (["A","B"]) into a JSON string literal ("[\"A\",\"B\"]") so it unmarshals
// into types.Task.Dependencies (*string). Values that are already a string (or
// null/absent) are passed through unchanged.
func normalizeTaskDependencies(rawTask json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawTask, &fields); err != nil {
		return nil, err
	}

	dep, ok := fields["dependencies"]
	if !ok {
		return rawTask, nil
	}
	trimmed := bytes.TrimSpace(dep)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return rawTask, nil // already a string (or null) — leave as-is
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(compact.String())
	if err != nil {
		return nil, err
	}
	fields["dependencies"] = encoded
	return json.Marshal(fields)
}

func buildGoalPrompt(goalReq types.GoalRequest) string {
	prompt := fmt.Sprintf(`
Based on the following goal information, create a detailed roadmap:

Goal: %s
Description: %s
Importance: %s
Target Completion: %s
Weekly Time Commitment: %s
Work Frequency: %s
Constraints: %s
Current Skill Level: %s
`, goalReq.GoalName, goalReq.GoalDescription, goalReq.GoalImportance,
		goalReq.TargetCompletionDate, goalReq.WeeklyTimeCommitment,
		goalReq.WorkFrequency, goalReq.Constraints, goalReq.CurrentSkillLevel)

	if goalReq.AdditionalContext != "" {
		prompt += fmt.Sprintf("Additional Context: %s\n", goalReq.AdditionalContext)
	}

	return prompt
}

func buildMilestoneContext(milestone *types.Milestone) string {
	if milestone == nil {
		return "No milestone context available"
	}

	context := fmt.Sprintf("Milestone: %s\n", milestone.SubGoal)
	context += fmt.Sprintf("Description: %s\n", milestone.Description)

	if milestone.AcceptanceTest != "" {
		context += fmt.Sprintf("Acceptance Test: %s\n", milestone.AcceptanceTest)
	}

	if milestone.DurationDaysMin > 0 || milestone.DurationDaysMax > 0 {
		context += fmt.Sprintf("Duration: %d-%d days\n", milestone.DurationDaysMin, milestone.DurationDaysMax)
	}

	if milestone.ResourcesForecast != nil && *milestone.ResourcesForecast != "" {
		context += fmt.Sprintf("Resources: %s\n", *milestone.ResourcesForecast)
	}

	if len(milestone.Spikes) > 0 {
		context += "Spikes to investigate: "
		for _, spike := range milestone.Spikes {
			context += fmt.Sprintf("%s (%d days); ", spike.Description, spike.TimeboxDays)
		}
		context += "\n"
	}

	if len(milestone.Risks) > 0 {
		context += "Risks to consider: "
		for _, risk := range milestone.Risks {
			context += fmt.Sprintf("%s [%s]; ", risk.Description, risk.Severity)
		}
		context += "\n"
	}

	return context
}

func buildReviewContext(review types.MilestoneReviewAnswers) string {
	out := ""
	appendIf := func(label, value string) {
		if value == "" {
			return
		}
		out += fmt.Sprintf("- %s: %s\n", label, value)
	}
	appendIf("Completion status", review.CompletionStatus)
	appendIf("What worked well", review.WhatWorked)
	appendIf("What didn't work", review.WhatDidntWork)
	appendIf("Obstacles", review.Obstacles)
	appendIf("Workload/pace feedback", review.PaceFeedback)
	appendIf("Instruction clarity", review.ClarityFeedback)
	appendIf("Preferences for next milestone / life changes", review.GoingForward)
	if out == "" {
		return "(User did not provide review feedback.)"
	}
	return out
}

func applyAdjustments(milestone *types.Milestone, adj *types.MilestoneAdjustments) {
	if adj.SubGoal != "" {
		milestone.SubGoal = adj.SubGoal
	}
	if adj.Description != "" {
		milestone.Description = adj.Description
	}
	if adj.AcceptanceTest != "" {
		milestone.AcceptanceTest = adj.AcceptanceTest
	}
	if adj.DurationDaysMin > 0 {
		milestone.DurationDaysMin = adj.DurationDaysMin
	}
	if adj.DurationDaysMax > 0 {
		milestone.DurationDaysMax = adj.DurationDaysMax
	}
	if adj.ResourcesForecast != "" {
		rf := adj.ResourcesForecast
		milestone.ResourcesForecast = &rf
	}
	if len(adj.Spikes) > 0 {
		milestone.Spikes = adj.Spikes
	}
	if len(adj.Risks) > 0 {
		milestone.Risks = adj.Risks
	}
}

// ─── System Prompts ───────────────────────────────────────────────────────────

func strategicRoadmapSystemPrompt() string {
	return `You are a Goal Achievement Coach Agent focused on creating strategic roadmaps.

CORE DIRECTIVES:
- Be autonomous: Make informed assumptions rather than asking for clarification
- Be thorough: Create comprehensive, actionable plans
- Be realistic: Provide achievable timelines with buffer for learning curves
- Be empowering: Frame guidance as enabling the user, not prescribing

YOUR TASK - STRATEGIC PLANNING:
Generate a high-level strategic roadmap with 2-4 major phases representing the journey from current state to goal achievement.

For each phase, create 2-5 milestones that represent significant achievements. Each milestone must include:
- sub_goal: Short, concise milestone name (no more than 5 words)
- description: Informative but not overly descriptive
- acceptance_test: Clear acceptance test (how to know it's complete)
- duration_days_min/max: Duration estimates (min/max days, accounting for learning curves)
- day: The day number when this milestone should start (sequential across the whole roadmap)
- resources_forecast: Resources forecast (tools, materials, courses needed as JSON string)
- spikes: Time-boxed investigations for unknowns (use empty array if none needed)
- risks: Potential obstacles with severity level (use empty array if no significant risks)

STRATEGIC PRINCIPLES:
1. Start with fundamentals before advanced concepts
2. Build milestones that validate progress before moving forward
3. Include learning time - people need to absorb and practice
4. Anticipate common obstacles and plan mitigations
5. Balance ambition with achievability

ASSUMPTIONS POLICY:
If the goal is vague, assume the most common interpretation and proceed. Document your assumptions in the phase descriptions.

Resources forecast should be a JSON string like: {"books": ["Book Title"], "tools": ["Tool Name"], "courses": ["Course Name"], "budget": "$X"}

This is the STRATEGIC VIEW - keep it high-level. Do NOT include tasks or steps.

You MUST call the generate_strategic_roadmap tool with your response.`
}

func milestoneTasksSystemPrompt(milestoneContext, reviewContext string) string {
	reviewSection := ""
	if reviewContext != "" {
		reviewSection = fmt.Sprintf(`

USER REVIEW OF PRIOR MILESTONE (adjust pacing/clarity/approach accordingly):
%s
`, reviewContext)
	}

	return fmt.Sprintf(`You are a Goal Achievement Coach Agent focused on creating detailed, actionable tactical plans.

CORE DIRECTIVES:
- Be thorough: Break down complex actions into clear, achievable steps
- Be specific: Every task should have concrete actions, not vague suggestions
- Be realistic: Account for learning time, practice, and potential setbacks
- Be empowering: Include rationales that help users understand WHY each task matters

MILESTONE CONTEXT:
%s%s

YOUR TASK - GENERATE TASKS:
Generate 10-14 specific, actionable tasks for this milestone.

EACH TASK MUST INCLUDE:
- Title: Clear, action-oriented (no more than 5 words)
- Description: What this task involves and how to approach it
- Rationale: Why this task matters for the overall goal
- Estimated minutes: Realistic time estimate (account for learning curve)
- Dependencies: JSON array of prerequisite task titles (e.g., ["Task 1 Title"] or [] for none)
- Confidence: Your confidence in the estimate (low/medium/high)
- Day: Which day in the milestone this task should be done (spread across 7-14 days)
- 2-4 detailed steps: Granular instructions that remove ambiguity

TASK DESIGN PRINCIPLES:
1. Start with foundational/easier tasks, progress to more complex
2. Include both "learning" and "doing" tasks
3. Build in practice and repetition for skill development
4. Add checkpoint/validation tasks every 3-4 tasks
5. Make dependencies realistic - don't over-constrain the sequence
6. Spread tasks across days to allow for learning and practice time

FORMATTING:
- Set all 'completed' fields to false
- Day numbers should be sequential starting from day 1
- Dependencies should be exact task titles from earlier tasks in the list

You MUST call the generate_milestone_tasks tool with your response.`, milestoneContext, reviewSection)
}

func adjustmentsSystemPrompt(milestoneContext, reviewContext string) string {
	return fmt.Sprintf(`You are a Goal Achievement Coach Agent reviewing a user's feedback on a completed milestone.

YOUR TASK:
Decide whether the NEXT milestone's strategic fields should be adjusted based on the user's review of the PRIOR milestone.

NEXT MILESTONE (as currently planned):
%s

USER REVIEW OF PRIOR MILESTONE:
%s

OUTPUT RULES:
- Set "adjusted" to true ONLY if at least one substantive change is being made
- For fields you do NOT want to change, use empty values ("" for strings, 0 for integers, [] for arrays)
- For fields you DO want to change, emit the new value
- "rationale": a one-sentence explanation of what you adjusted and why (or "" if no adjustments)

GUIDELINES:
- Respect the user's pace preference (if they want slower pacing, increase duration)
- If instructions were unclear, consider clarifying the description or acceptance_test
- Keep changes minimal and proportionate to the feedback
- If the user did not provide review feedback, set "adjusted" to false

You MUST call the generate_milestone_adjustments tool with your response.`, milestoneContext, reviewContext)
}

// ─── Tool Definitions ─────────────────────────────────────────────────────────

func strategicRoadmapTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name:        "generate_strategic_roadmap",
		Description: param.NewOpt("Generate a strategic roadmap with phases and milestones"),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"goal": map[string]interface{}{
					"type":        "string",
					"description": "Overall goal title",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Description of the roadmap",
				},
				"days": map[string]interface{}{
					"type":        "integer",
					"description": "Total estimated days",
				},
				"phases": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":           map[string]interface{}{"type": "string"},
							"description":    map[string]interface{}{"type": "string"},
							"order_index":    map[string]interface{}{"type": "integer"},
							"duration_weeks": map[string]interface{}{"type": "string"},
							"milestones": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"sub_goal":           map[string]interface{}{"type": "string"},
										"description":        map[string]interface{}{"type": "string"},
										"acceptance_test":    map[string]interface{}{"type": "string"},
										"duration_days_min":  map[string]interface{}{"type": "integer"},
										"duration_days_max":  map[string]interface{}{"type": "integer"},
										"order_index":        map[string]interface{}{"type": "integer"},
										"day":                map[string]interface{}{"type": "integer"},
										"resources_forecast": map[string]interface{}{"type": "string"},
										"spikes": map[string]interface{}{
											"type": "array",
											"items": map[string]interface{}{
												"type": "object",
												"properties": map[string]interface{}{
													"description":  map[string]interface{}{"type": "string"},
													"timebox_days": map[string]interface{}{"type": "integer"},
												},
											},
										},
										"risks": map[string]interface{}{
											"type": "array",
											"items": map[string]interface{}{
												"type": "object",
												"properties": map[string]interface{}{
													"description": map[string]interface{}{"type": "string"},
													"severity":    map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			Required: []string{"goal", "description", "days", "phases"},
		},
	}
}

func milestoneTasksTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name:        "generate_milestone_tasks",
		Description: param.NewOpt("Generate detailed tasks for a milestone"),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"title":        map[string]interface{}{"type": "string"},
							"description":  map[string]interface{}{"type": "string"},
							"rationale":    map[string]interface{}{"type": "string"},
							"est_minutes":  map[string]interface{}{"type": "integer"},
							"dependencies": map[string]interface{}{"type": "string"},
							"confidence":   map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}},
							"day":          map[string]interface{}{"type": "integer"},
							"completed":    map[string]interface{}{"type": "boolean"},
							"steps": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"description": map[string]interface{}{"type": "string"},
										"completed":   map[string]interface{}{"type": "boolean"},
										"number":      map[string]interface{}{"type": "integer"},
									},
								},
							},
						},
					},
				},
			},
			Required: []string{"tasks"},
		},
	}
}

func adjustmentsTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name:        "generate_milestone_adjustments",
		Description: param.NewOpt("Generate adjustments for a milestone based on user review"),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"adjusted": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether any substantive changes are being made",
				},
				"sub_goal": map[string]interface{}{
					"type":        "string",
					"description": "New milestone name (empty if unchanged)",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "New description (empty if unchanged)",
				},
				"acceptance_test": map[string]interface{}{
					"type":        "string",
					"description": "New acceptance test (empty if unchanged)",
				},
				"duration_days_min": map[string]interface{}{
					"type":        "integer",
					"description": "New min duration (0 if unchanged)",
				},
				"duration_days_max": map[string]interface{}{
					"type":        "integer",
					"description": "New max duration (0 if unchanged)",
				},
				"resources_forecast": map[string]interface{}{
					"type":        "string",
					"description": "New resources forecast (empty if unchanged)",
				},
				"spikes": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"description":  map[string]interface{}{"type": "string"},
							"timebox_days": map[string]interface{}{"type": "integer"},
						},
					},
				},
				"risks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"description": map[string]interface{}{"type": "string"},
							"severity":    map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high"}},
						},
					},
				},
				"rationale": map[string]interface{}{
					"type":        "string",
					"description": "Explanation of changes (empty if no adjustments)",
				},
			},
			Required: []string{"adjusted", "rationale"},
		},
	}
}
