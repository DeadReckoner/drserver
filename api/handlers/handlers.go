package handlers

import (
	"dr/server/auth/apple"
	"dr/server/storage"

	"gorm.io/gorm"
)

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
	Billing      *BillingHandler
	Admin        *AdminHandler
}

// NewHandlers creates all handlers with the given database connection
func NewHandlers(db *gorm.DB, storageClient *storage.StorageClient, appleClient *apple.Client) *Handlers {
	return &Handlers{
		User:         NewUserHandler(db, storageClient, appleClient),
		Friend:       NewFriendHandler(db),
		Roadmap:      NewRoadmapHandler(db),
		Habit:        NewHabitHandler(db),
		Task:         NewTaskHandler(db),
		Agent:        NewAgentHandler(db),
		Notification: NewNotificationHandler(db),
		Message:      NewMessageHandler(db),
		Feedback:     NewFeedbackHandler(db),
		Nudge:        NewNudgeHandler(db),
		Billing:      NewBillingHandler(db),
		Admin:        NewAdminHandler(db),
	}
}
