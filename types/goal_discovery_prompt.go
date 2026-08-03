package types

import (
	"fmt"
	"strings"
)

// GoalOptionsPromptVersion identifies the prompt that produced a result. Stored
// on the job result so a bad batch can be traced to the exact copy that made it.
// Bump on any material change to the text below.
//
// Lives beside AIAckText/CurrentAIAckVersion: this package is already where
// versioned AI-facing copy is centralised, and both the OpenAI and Anthropic
// providers import it, so neither needs its own duplicate of this prompt.
const GoalOptionsPromptVersion = "goal_options.v1"

// GoalOptionsMaxOutputTokens bounds the model's completion. Three options with
// three effort lanes each fits comfortably; anything larger is a runaway.
const GoalOptionsMaxOutputTokens = 4096

// GoalOptionsSystemPrompt is the instruction set for goal-option generation.
// Structure of the output is enforced separately by the provider's schema/tool
// definition and by ValidateGoalOptions — this text covers judgement, not shape.
func GoalOptionsSystemPrompt() string {
	return `You are the Dead Reckoner goal strategist. A user knows something in their life
needs to change but does not yet know WHICH goal to commit to. You are given their
questionnaire answers. Propose exactly three candidate goals.

RANKING
- Rank 1 is the best fit for the answers you were given, rank 2 next, rank 3 last.
- Rank by fit to THIS user's stated area, constraints, self-description and desired
  outcomes — not by how impressive the goal sounds.

THE THREE OPTIONS MUST BE MATERIALLY DIFFERENT
- Different underlying activity, not the same goal reworded three times.
- Different titles. If two options would produce nearly the same roadmap, replace one.

EACH GOAL MUST BE CONCRETE
- A specific, measurable target a roadmap can be built from.
- target_outcome must state what is objectively true when the goal is complete
  (a number, an artefact, a demonstrable capability).
- Reject vague aims like "be happier", "get in shape" or "be more successful";
  turn them into something checkable instead.
- why_this_fits must reference what the user actually said. Do not assert facts
  about the user they did not tell you, and do not claim certainty about their
  motives, diagnosis or circumstances.

EFFORT EXAMPLES ARE ESTIMATES, NOT PROMISES
- Give exactly three lanes per option: focused, steady, light.
- focused is the highest weekly commitment, light the lowest; steady sits between.
- Weekly hours must never increase as the pace gets lighter.
- A lighter weekly commitment must never finish sooner than a heavier one.
- display_text is one short human sentence, e.g. "About 8-10 hours a week, roughly 4-5 months".
- Phrase these as examples of what the pace would look like, never as guarantees.

SAFETY
- Do not propose illegal, dangerous, exploitative or self-harming goals.
- No crash diets, extreme calorie restriction, or prescriptive exercise programming.
- No get-rich-quick, trading, gambling or speculative income schemes.
- No medical, psychiatric, legal or financial diagnosis or advice, and no goal whose
  steps require professional judgement the user is not qualified to exercise.
- For health, mental-health, legal or financial subject matter, stay inside ordinary
  goal-setting: habits, learning, preparation, and consulting a qualified professional.
- If the answers indicate a crisis, self-harm, or imminent harm to the user or anyone
  else, do NOT return goal options. Set safety_blocked to true and give a brief
  neutral reason instead.`
}

// GoalOptionsUserPrompt renders the questionnaire, any prior-option exclusions,
// and an optional repair hint into the user turn.
//
// exclude carries the titles of options the user already rejected via "Show me
// different options" so a regeneration is genuinely different rather than a
// reshuffle. repairHint is non-empty only on a bounded retry after the previous
// attempt failed ValidateGoalOptions.
func GoalOptionsUserPrompt(answers GoalDiscoveryAnswers, exclude []string, repairHint string) string {
	var sb strings.Builder

	sb.WriteString("The user answered the goal discovery questionnaire as follows.\n\n")
	sb.WriteString(fmt.Sprintf("What made them open the app: %s\n", answers.Trigger))
	if answers.TriggerNote != "" {
		sb.WriteString(fmt.Sprintf("Their own words on that: %s\n", answers.TriggerNote))
	}

	sb.WriteString(fmt.Sprintf("Areas that feel below standard: %s\n", joinAreas(answers.Areas)))
	if answers.CustomArea != "" {
		sb.WriteString(fmt.Sprintf("Additional area they named themselves: %s\n", answers.CustomArea))
	}

	sb.WriteString(fmt.Sprintf("How they describe themselves right now: %s\n", answers.SelfDescription))
	sb.WriteString(fmt.Sprintf("What they keep thinking about but not acting on: %s\n", answers.RecurringThought))
	sb.WriteString(fmt.Sprintf("What would make the next few months feel successful: %s\n", answers.SuccessDefinition))

	sb.WriteString(fmt.Sprintf("What they want the goal to do for them: %s\n", joinOutcomes(answers.Outcomes)))
	if answers.CustomOutcome != "" {
		sb.WriteString(fmt.Sprintf("Additional outcome they named themselves: %s\n", answers.CustomOutcome))
	}

	sb.WriteString(fmt.Sprintf("The kind of target they say sounds right: %s\n", answers.TargetType))

	if len(exclude) > 0 {
		sb.WriteString("\nThe user has already seen and rejected these goals. Do not propose them again, " +
			"and do not propose near-synonyms of them. Explore a different angle:\n")
		for _, title := range exclude {
			if strings.TrimSpace(title) == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s\n", title))
		}
	}

	if repairHint != "" {
		sb.WriteString("\nYour previous attempt was rejected by schema validation. Fix exactly these problems " +
			"and return a complete, valid set of three options:\n")
		sb.WriteString(repairHint)
		sb.WriteString("\n")
	}

	sb.WriteString("\nReturn exactly three ranked, materially different goal options.")
	return sb.String()
}

// GoalOptionsSchemaRequired lists the top-level keys the model must always
// return. OpenAI strict mode requires every property to be listed here.
func GoalOptionsSchemaRequired() []string {
	return []string{"safety_blocked", "safety_reason", "options"}
}

// GoalOptionsSchemaProperties is the JSON-Schema property map for the goal
// options payload. Shared by the OpenAI strict schema and the Anthropic tool
// definition so the two backends cannot describe different contracts.
func GoalOptionsSchemaProperties() map[string]any {
	effortItem := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pace": map[string]any{"type": "string", "enum": []string{
				string(PaceFocused), string(PaceSteady), string(PaceLight),
			}},
			"hours_per_week_min":            map[string]any{"type": "number", "description": "Lower bound of weekly hours for this pace"},
			"hours_per_week_max":            map[string]any{"type": "number", "description": "Upper bound of weekly hours for this pace"},
			"estimated_duration_months_min": map[string]any{"type": "number", "description": "Optimistic months to reach the target outcome at this pace"},
			"estimated_duration_months_max": map[string]any{"type": "number", "description": "Pessimistic months to reach the target outcome at this pace"},
			"display_text":                  map[string]any{"type": "string", "description": "One short sentence describing this pace as an example, not a promise"},
		},
		"required": []string{
			"pace", "hours_per_week_min", "hours_per_week_max",
			"estimated_duration_months_min", "estimated_duration_months_max", "display_text",
		},
		"additionalProperties": false,
	}

	optionItem := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rank":               map[string]any{"type": "integer", "description": "1 = best fit, 2, or 3. Unique across the set."},
			"goal_title":         map[string]any{"type": "string", "description": "Specific, concrete goal. Distinct from the other options."},
			"why_this_fits":      map[string]any{"type": "string", "description": "Why this fits what the user actually said"},
			"target_outcome":     map[string]any{"type": "string", "description": "What is objectively true when the goal is complete"},
			"why_worth_pursuing": map[string]any{"type": "string", "description": "What the user gains by committing to it"},
			"effort_examples": map[string]any{
				"type":        "array",
				"description": "Exactly three lanes: one focused, one steady, one light",
				"items":       effortItem,
			},
			"difficulty": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level": map[string]any{"type": "string", "enum": []string{
						string(DifficultyStarter), string(DifficultyModerate),
						string(DifficultyDemanding), string(DifficultyIntense),
					}},
					"explanation": map[string]any{"type": "string", "description": "Why the goal sits at that difficulty for this user"},
				},
				"required":             []string{"level", "explanation"},
				"additionalProperties": false,
			},
			"accountability_strength": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"level": map[string]any{"type": "string", "enum": []string{
						string(AccountabilityLow), string(AccountabilityModerate), string(AccountabilityHigh),
					}},
					"explanation": map[string]any{"type": "string", "description": "How much external accountability this goal needs to stick"},
				},
				"required":             []string{"level", "explanation"},
				"additionalProperties": false,
			},
		},
		"required": []string{
			"rank", "goal_title", "why_this_fits", "target_outcome",
			"why_worth_pursuing", "effort_examples", "difficulty", "accountability_strength",
		},
		"additionalProperties": false,
	}

	return map[string]any{
		"safety_blocked": map[string]any{
			"type":        "boolean",
			"description": "True only when the answers indicate crisis or imminent harm. Leave options empty when true.",
		},
		"safety_reason": map[string]any{
			"type":        "string",
			"description": "Brief neutral reason when safety_blocked is true, otherwise an empty string.",
		},
		"options": map[string]any{
			"type":        "array",
			"description": "Exactly three ranked, materially different goal options. Empty when safety_blocked is true.",
			"items":       optionItem,
		},
	}
}

func joinAreas(areas []GoalDiscoveryArea) string {
	if len(areas) == 0 {
		return "(none selected)"
	}
	parts := make([]string, 0, len(areas))
	for _, a := range areas {
		parts = append(parts, string(a))
	}
	return strings.Join(parts, ", ")
}

func joinOutcomes(outcomes []GoalDiscoveryOutcome) string {
	if len(outcomes) == 0 {
		return "(none selected)"
	}
	parts := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		parts = append(parts, string(o))
	}
	return strings.Join(parts, ", ")
}
