package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

// Tool name constants
const (
	ToolCreateRoadmap     = "create_roadmap"
	ToolEditRoadmap       = "edit_roadmap"
	ToolPerformanceReport = "performance_report"
)

// RoadmapOutput is the structured output of the create_roadmap tool.
// Matches the existing dual-map architecture.
type RoadmapOutput struct {
	GoalTitle    string       `json:"goal_title"`
	GoalSummary  string       `json:"goal_summary"`
	TotalWeeks   int          `json:"total_weeks"`
	MacroMap     MacroMap     `json:"macro_map"`
	MilestoneMap MilestoneMap `json:"milestone_map"`
}

// MacroMap represents the high-level strategic view of the roadmap.
type MacroMap struct {
	Phases []MacroPhase `json:"phases"`
}

// MacroPhase represents a major phase in the roadmap.
type MacroPhase struct {
	PhaseNumber   int      `json:"phase_number"`
	Title         string   `json:"title"`
	DurationWeeks int      `json:"duration_weeks"`
	Objectives    []string `json:"objectives"`
	Milestones    []string `json:"milestones"`
}

// MilestoneMap represents the tactical execution view.
type MilestoneMap struct {
	CurrentMilestone MilestoneDetail  `json:"current_milestone"`
	NextMilestone    *MilestoneDetail `json:"next_milestone,omitempty"`
}

// MilestoneDetail represents a detailed milestone with tasks and habits.
type MilestoneDetail struct {
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	DurationDays int     `json:"duration_days"`
	Tasks        []Task  `json:"tasks"`
	Habits       []Habit `json:"habits"`
}

// Task represents a single task within a milestone.
type Task struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"` // "high", "medium", "low"
}

// Habit represents a recurring habit within a milestone.
type Habit struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	FrequencyMask int    `json:"frequency_mask"` // bitmask: Sun=1,Mon=2,Tue=4,Wed=8,Thu=16,Fri=32,Sat=64
	DurationMins  int    `json:"duration_mins"`
}

// RoadmapEditOutput is the structured output of the edit_roadmap tool.
type RoadmapEditOutput struct {
	EditSummary string          `json:"edit_summary"` // Human-readable summary of what changed
	Changes     []RoadmapChange `json:"changes"`
}

// RoadmapChange represents a single edit to the roadmap.
type RoadmapChange struct {
	ChangeType  string `json:"change_type"` // "add_task", "remove_task", "modify_habit", "adjust_timeline", etc.
	Target      string `json:"target"`      // Which milestone/phase this applies to
	Description string `json:"description"`
	Data        any    `json:"data,omitempty"` // The new/updated object if applicable
}

// PerformanceReportOutput is the structured output of the performance_report tool.
type PerformanceReportOutput struct {
	OverallScore        int            `json:"overall_score"` // 0-100
	Summary             string         `json:"summary"`
	Highlights          []string       `json:"highlights"`
	AreasForImprovement []string       `json:"areas_for_improvement"`
	HabitInsights       []HabitInsight `json:"habit_insights"`
	MilestoneStatus     string         `json:"milestone_status"` // "on_track", "at_risk", "behind"
	Recommendations     []string       `json:"recommendations"`
	NextFocusAreas      []string       `json:"next_focus_areas"`
}

// HabitInsight provides analysis of a single habit's performance.
type HabitInsight struct {
	HabitTitle     string  `json:"habit_title"`
	CompletionRate float64 `json:"completion_rate"` // 0.0-1.0
	Trend          string  `json:"trend"`           // "improving", "stable", "declining"
	Note           string  `json:"note"`
}

// getToolsForMode returns the appropriate tools for the given agent mode.
func getToolsForMode(mode AgentMode) []anthropic.ToolParam {
	switch mode {
	case ModeRoadmapGeneration:
		return []anthropic.ToolParam{createRoadmapTool()}
	case ModeRoadmapEditor:
		return []anthropic.ToolParam{editRoadmapTool()}
	case ModePerformanceCoach:
		return []anthropic.ToolParam{performanceReportTool()}
	default:
		return nil
	}
}

func createRoadmapTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name: ToolCreateRoadmap,
		Description: param.NewOpt(`Generate a complete, structured goal roadmap after gathering sufficient
information from the user. Only call this tool when you have enough context about:
- The user's specific goal
- Their current skill/experience level
- Available time per week
- Their timeline/deadline (if any)
- Key constraints or preferences
The roadmap must include a macro map (full project phases) and a milestone map (first 10-14 day sprint).`),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"goal_title": map[string]interface{}{
					"type":        "string",
					"description": "Concise title for the goal",
				},
				"goal_summary": map[string]interface{}{
					"type":        "string",
					"description": "2-3 sentence summary of the goal and approach",
				},
				"total_weeks": map[string]interface{}{
					"type":        "integer",
					"description": "Estimated total weeks to achieve the goal",
				},
				"macro_map": map[string]interface{}{
					"type":        "object",
					"description": "High-level phases covering the full project duration",
					"properties": map[string]interface{}{
						"phases": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"phase_number":   map[string]interface{}{"type": "integer"},
									"title":          map[string]interface{}{"type": "string"},
									"duration_weeks": map[string]interface{}{"type": "integer"},
									"objectives":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
									"milestones":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								},
							},
						},
					},
				},
				"milestone_map": map[string]interface{}{
					"type":        "object",
					"description": "Tactical 10-14 day execution view with tasks and habits",
					"properties": map[string]interface{}{
						"current_milestone": milestoneDetailSchema(),
						"next_milestone":    milestoneDetailSchema(),
					},
				},
			},
			Required: []string{"goal_title", "goal_summary", "total_weeks", "macro_map", "milestone_map"},
		},
	}
}

func editRoadmapTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name: ToolEditRoadmap,
		Description: param.NewOpt(`Apply structured edits to an existing roadmap based on user feedback.
Use this after understanding exactly what the user wants to change. Can modify tasks,
habits, timelines, phases, or milestone content. Always explain changes in edit_summary.`),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"edit_summary": map[string]interface{}{
					"type":        "string",
					"description": "Plain English summary of all changes made",
				},
				"changes": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"change_type": map[string]interface{}{
								"type": "string",
								"enum": []string{
									"add_task", "remove_task", "modify_task",
									"add_habit", "remove_habit", "modify_habit",
									"adjust_timeline", "rename_milestone",
									"modify_phase", "reorder_tasks",
								},
							},
							"target":      map[string]interface{}{"type": "string"},
							"description": map[string]interface{}{"type": "string"},
						},
						"required": []string{"change_type", "target", "description"},
					},
				},
			},
			Required: []string{"edit_summary", "changes"},
		},
	}
}

func performanceReportTool() anthropic.ToolParam {
	return anthropic.ToolParam{
		Name: ToolPerformanceReport,
		Description: param.NewOpt(`Generate a structured performance report and coaching analysis based on
the user's habit completion data, milestone progress, and goal activity.
Use this after providing a conversational performance update to the user.`),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]interface{}{
				"overall_score": map[string]interface{}{
					"type":        "integer",
					"minimum":     0,
					"maximum":     100,
					"description": "Overall performance score 0-100",
				},
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "2-3 sentence overall performance summary",
				},
				"highlights":            stringArraySchema("What the user did well"),
				"areas_for_improvement": stringArraySchema("What needs attention"),
				"milestone_status": map[string]interface{}{
					"type": "string",
					"enum": []string{"on_track", "at_risk", "behind", "ahead"},
				},
				"recommendations":  stringArraySchema("Specific actionable recommendations"),
				"next_focus_areas": stringArraySchema("Top 2-3 areas to focus on next"),
				"habit_insights": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"habit_title":     map[string]interface{}{"type": "string"},
							"completion_rate": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
							"trend":           map[string]interface{}{"type": "string", "enum": []string{"improving", "stable", "declining"}},
							"note":            map[string]interface{}{"type": "string"},
						},
					},
				},
			},
			Required: []string{"overall_score", "summary", "highlights", "milestone_status", "recommendations"},
		},
	}
}

func milestoneDetailSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title":         map[string]interface{}{"type": "string"},
			"description":   map[string]interface{}{"type": "string"},
			"duration_days": map[string]interface{}{"type": "integer"},
			"tasks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":       map[string]interface{}{"type": "string"},
						"description": map[string]interface{}{"type": "string"},
						"priority":    map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}},
					},
				},
			},
			"habits": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":          map[string]interface{}{"type": "string"},
						"description":    map[string]interface{}{"type": "string"},
						"frequency_mask": map[string]interface{}{"type": "integer", "description": "Bitmask: Sun=1,Mon=2,Tue=4,Wed=8,Thu=16,Fri=32,Sat=64"},
						"duration_mins":  map[string]interface{}{"type": "integer"},
					},
				},
			},
		},
	}
}

func stringArraySchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": description,
		"items":       map[string]interface{}{"type": "string"},
	}
}
