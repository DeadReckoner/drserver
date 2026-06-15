package handlers

import (
	"context"
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/notifications"
	"dr/server/types"
	"dr/server/websocket"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type MessageHandler struct {
	db *gorm.DB
}

func NewMessageHandler(db *gorm.DB) *MessageHandler {
	return &MessageHandler{db: db}
}

// Thread-specific websocket management
type ThreadRoom struct {
	clients   map[*gorillaws.Conn]*Client
	broadcast chan *types.Messages
	mutex     sync.Mutex
}

type Client struct {
	UserID   string
	ThreadID string
}

// WebSocket message types
type IncomingMessage struct {
	Content string `json:"content"`
}

type OutgoingMessage struct {
	MessageID string `json:"message_id"`
	ThreadID  string `json:"thread_id"`
	UserID    string `json:"user_id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// Map of threadID -> ThreadRoom
var rooms = make(map[string]*ThreadRoom)
var roomsMutex = &sync.Mutex{}

// ============================================================================
// REST API Handlers for Threads and Messages
// ============================================================================

// GetThreads returns all threads for the current user
func (h *MessageHandler) GetThreads(c *gin.Context) {
	ctx := c.Request.Context()
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	threads, err := database.GetThreadsByUser(c, userID.(string))
	if err != nil {
		log.Error(ctx, "Failed to get threads", err)
		utils.InternalErrorResponse(c, err)
		return
	}

	// Build response with participants for each thread
	var response []types.ThreadWithParticipants
	for _, thread := range threads {
		participants, err := database.GetThreadParticipants(c, thread.ThreadID)
		if err != nil {
			log.Error(ctx, "Failed to get participants for thread", thread.ThreadID, err)
			continue
		}

		lastSentMessage := ""
		var lastMessage *types.MessageResponse
		if thread.LastSentMessage != nil {
			lastSentMessage = *thread.LastSentMessage
			lastMessage, err = database.GetMessageByID(c, lastSentMessage, userID.(string))
			if err != nil {
				log.Error(c, err.Error())
			}
		}

		response = append(response, types.ThreadWithParticipants{
			ThreadID:        thread.ThreadID,
			Name:            thread.Name,
			Participants:    participants,
			TotalUnread:     thread.TotalUnread,
			LastSentMessage: lastSentMessage,
			CreatedAt:       thread.CreatedAt,
			UpdatedAt:       thread.UpdatedAt,
			LastMessage:     lastMessage,
		})
	}

	utils.DataResponse(c, http.StatusOK, response)
	log.Info(ctx, fmt.Sprintf("Retrieved %d threads for user %s", len(response), userID))
}

// GetMessages returns all messages for a specific thread
func (h *MessageHandler) GetMessages(c *gin.Context) {
	ctx := c.Request.Context()
	threadID := c.Param("threadid")
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	messages, err := database.GetThreadMessages(c, threadID, userID.(string))
	if err != nil {
		if err.Error() == "access denied: user is not a participant of this thread" {
			utils.ForbiddenResponse(c, "You are not a participant of this thread")
			return
		}
		log.Error(ctx, "Failed to get messages for thread", threadID, err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, messages)
	log.Info(ctx, fmt.Sprintf("Retrieved %d messages from thread %s for user %s", len(messages), threadID, userID))
}

// ============================================================================
// WebSocket Handlers
// ============================================================================

// getOrCreateRoom gets an existing room or creates a new one for a thread
func getOrCreateRoom(threadID string) *ThreadRoom {
	roomsMutex.Lock()
	defer roomsMutex.Unlock()

	room, exists := rooms[threadID]
	if !exists {
		room = &ThreadRoom{
			clients:   make(map[*gorillaws.Conn]*Client),
			broadcast: make(chan *types.Messages, 256),
		}
		rooms[threadID] = room
		// Start the broadcast handler for this room
		go room.handleBroadcast()
	}
	return room
}

// handleBroadcast handles broadcasting messages to all clients in a thread room
func (room *ThreadRoom) handleBroadcast() {
	for {
		message := <-room.broadcast

		// Convert message to JSON
		outMsg := OutgoingMessage{
			MessageID: message.MessageID,
			ThreadID:  message.ThreadID,
			UserID:    message.UserID,
			Content:   message.Content,
			CreatedAt: message.CreatedAt.Format("2006-01-02T15:04:05.999Z07:00"),
		}

		msgBytes, err := json.Marshal(outMsg)
		if err != nil {
			log.Error(context.Background(), "Error marshaling message", err)
			continue
		}

		room.mutex.Lock()
		for conn := range room.clients {
			err := conn.WriteMessage(gorillaws.TextMessage, msgBytes)
			if err != nil {
				log.Error(context.Background(), "Error sending message to client", err)
				_ = conn.Close()
				delete(room.clients, conn)
			}
		}
		room.mutex.Unlock()
	}
}

// WSHandler handles WebSocket connections for a specific thread
func (h *MessageHandler) WSHandler(c *gin.Context) {
	ctx := c.Request.Context()
	threadID := c.Param("threadid")

	// Get authenticated user
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		log.Warn(ctx, "WebSocket error: missing user_id")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userIDStr := userID.(string)

	// Verify user is a participant of this thread
	err := database.VerifyThreadParticipant(c, threadID, userIDStr)
	if err != nil {
		if err.Error() == "access denied: user is not a participant of this thread" {
			log.Warn(ctx, "WebSocket access denied: user not participant of thread", threadID)
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
		log.Error(ctx, "WebSocket error checking thread access", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Upgrade connection using shared upgrader
	conn, err := websocket.UpgradeConnection(c)
	if err != nil {
		log.Error(ctx, "WebSocket upgrade error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Get or create room for this thread
	room := getOrCreateRoom(threadID)

	// Register client
	client := &Client{
		UserID:   userIDStr,
		ThreadID: threadID,
	}

	room.mutex.Lock()
	room.clients[conn] = client
	room.mutex.Unlock()

	log.Info(ctx, fmt.Sprintf("WebSocket connected: user %s joined thread %s", userIDStr, threadID))

	// Read messages from client
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Info(ctx, "WebSocket read error", threadID, err)
			room.mutex.Lock()
			delete(room.clients, conn)
			room.mutex.Unlock()
			break
		}

		// Parse incoming message
		var inMsg IncomingMessage
		if err := json.Unmarshal(msgBytes, &inMsg); err != nil {
			log.Warn(ctx, "Error parsing message from client", err)
			// Send error back to client
			errMsg := map[string]string{"error": "invalid message format"}
			errBytes, _ := json.Marshal(errMsg)
			_ = conn.WriteMessage(gorillaws.TextMessage, errBytes)
			continue
		}

		// Validate content is not empty
		if inMsg.Content == "" {
			log.Warn(ctx, "Empty message content from client")
			errMsg := map[string]string{"error": "message content cannot be empty"}
			errBytes, _ := json.Marshal(errMsg)
			_ = conn.WriteMessage(gorillaws.TextMessage, errBytes)
			continue
		}

		// Save message to database
		savedMessage, err := database.CreateMessage(c, threadID, userIDStr, inMsg.Content)
		if err != nil {
			log.Error(ctx, "Error saving message to database", threadID, err)
			errMsg := map[string]string{"error": "failed to save message"}
			errBytes, _ := json.Marshal(errMsg)
			_ = conn.WriteMessage(gorillaws.TextMessage, errBytes)
			continue
		}

		// Broadcast persisted message to all clients in this thread room
		log.Info(ctx, "Broadcasting saved message", savedMessage.MessageID, threadID)
		room.broadcast <- &savedMessage

		// Send push notifications to other thread participants
		go sendMessageNotifications(threadID, userIDStr, savedMessage.MessageID, inMsg.Content)
	}

	log.Info(ctx, fmt.Sprintf("WebSocket disconnected: user %s left thread %s", userIDStr, threadID))
}

func (h *MessageHandler) GetTotalUnreadCount(c *gin.Context) {

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return
	}

	totalUnreadCount, _ := database.CalculateTotalUnreadCount(c, userID.(string))

	utils.DataResponse(c, http.StatusOK, totalUnreadCount)
}

// GetMessageById returns a single message by its ID
func (h *MessageHandler) GetMessageById(c *gin.Context) {
	ctx := c.Request.Context()
	messageID := c.Param("messageid")
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	message, err := database.GetMessageByID(c, messageID, userID.(string))
	if err != nil {
		if err.Error() == "message not found" {
			utils.NotFoundResponse(c, "Message not found")
			return
		}
		if err.Error() == "access denied: user is not a participant of this thread" {
			utils.ForbiddenResponse(c, "You are not a participant of this thread")
			return
		}
		log.Error(ctx, "Failed to get message", messageID, err)
		utils.InternalErrorResponse(c, err)
		return
	}

	utils.DataResponse(c, http.StatusOK, message)
	log.Info(ctx, fmt.Sprintf("Retrieved message %s for user %s", messageID, userID))
}

// sendMessageNotifications sends push notifications to all thread participants except the sender
func sendMessageNotifications(threadID, senderID, messageID, content string) {
	ctx := &gin.Context{}

	// Get all thread participants
	participants, err := database.GetThreadParticipants(ctx, threadID)
	if err != nil {
		log.Error(context.Background(), "Failed to get thread participants for notifications", err)
		return
	}

	// Get sender's name
	sender, err := database.GetUserByID(senderID)
	if err != nil {
		log.Error(context.Background(), "Failed to get sender info for notification", err)
		return
	}

	senderName := sender.FirstName + " " + sender.LastName

	// Send notification to all participants except the sender
	for _, participant := range participants {
		if participant.UserID == senderID {
			continue // Don't notify the sender
		}

		// Truncate content if too long
		displayContent := content
		if len(content) > 100 {
			displayContent = content[:100] + "..."
		}

		// FriendID is the sender so muting a friend mutes their messages.
		// message_new is cap-exempt and dropped (not deferred) in quiet hours
		// — see notifications.capExempt/quietHoursDrop.
		notifications.Dispatch(notifications.DispatchOptions{
			UserID:           participant.UserID,
			Category:         notifications.CategoryFAndA,
			NotificationType: "message_new",
			DedupKey:         messageID,
			FriendID:         senderID,
			Title:            senderName,
			Body:             displayContent,
			Data: map[string]any{
				"thread_id":  threadID,
				"message_id": messageID,
				"sender_id":  senderID,
			},
		})
	}
}
