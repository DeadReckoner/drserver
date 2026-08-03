package handlers

import (
	"dr/server/auth/apple"
	"dr/server/email"
	"dr/server/storage"

	"gorm.io/gorm"
)

// Handlers contains all handler instances with their dependencies injected
type Handlers struct {
	User                 *UserHandler
	Friend               *FriendHandler
	Roadmap              *RoadmapHandler
	Habit                *HabitHandler
	Task                 *TaskHandler
	Agent                *AgentHandler
	Notification         *NotificationHandler
	NotificationSettings *NotificationSettingsHandler
	Message              *MessageHandler
	Feedback             *FeedbackHandler
	Nudge                *NudgeHandler
	Billing              *BillingHandler
	Admin                *AdminHandler
	Webhook              *WebhookHandler
	Report               *ReportHandler
}

// NewHandlers creates all handlers with the given database connection
func NewHandlers(db *gorm.DB, storageClient *storage.StorageClient, appleClient *apple.Client) *Handlers {
	// One shared email dispatcher: cheap to construct, holds an HTTP client
	// and the parsed templates. Passed into any handler that fires
	// transactional emails.
	mailer := email.NewDispatcher(db)
	return &Handlers{
		User:                 NewUserHandler(db, storageClient, appleClient, mailer),
		Friend:               NewFriendHandler(db),
		Roadmap:              NewRoadmapHandler(db),
		Habit:                NewHabitHandler(db),
		Task:                 NewTaskHandler(db),
		Agent:                NewAgentHandler(db),
		Notification:         NewNotificationHandler(db),
		NotificationSettings: NewNotificationSettingsHandler(db),
		Message:              NewMessageHandler(db),
		Feedback:             NewFeedbackHandler(db, mailer),
		Nudge:                NewNudgeHandler(db),
		Billing:              NewBillingHandler(db),
		Admin:                NewAdminHandler(db),
		Webhook:              NewWebhookHandler(db, mailer),
		Report:               NewReportHandler(db, mailer),
	}
}
