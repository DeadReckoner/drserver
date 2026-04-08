package handlers

import "gorm.io/gorm"

// Handlers contains all handler instances with their dependencies injected
type Handlers struct {
	User         *UserHandler
	Friend       *FriendHandler
	Roadmap      *RoadmapHandler
	Habit        *HabitHandler
	Task         *TaskHandler
	Agent        *AgentHandler
	Notification *NotificationHandler
	Message      *MessageHandler
	Feedback     *FeedbackHandler
	Nudge        *NudgeHandler
	Metrics      *MetricsHandler
}

// NewHandlers creates all handlers with the given database connection
func NewHandlers(db *gorm.DB) *Handlers {
	return &Handlers{
		User:         NewUserHandler(db),
		Friend:       NewFriendHandler(db),
		Roadmap:      NewRoadmapHandler(db),
		Habit:        NewHabitHandler(db),
		Task:         NewTaskHandler(db),
		Agent:        NewAgentHandler(db),
		Notification: NewNotificationHandler(db),
		Message:      NewMessageHandler(db),
		Feedback:     NewFeedbackHandler(db),
		Nudge:        NewNudgeHandler(db),
		Metrics:      NewMetricsHandler(db),
	}
}
