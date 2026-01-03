package handlers

import (
	"dr/server/api/utils"
	"dr/server/database"
	"dr/server/log"
	"dr/server/types"
	"dr/server/websocket"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
)

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
func GetThreads(c *gin.Context) {
	ctx := c.Request.Context()
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		utils.BadRequestResponse(c, "missing or invalid user_id")
		return
	}

	threads, err := database.GetThreadsByUser(c, userID.(string))
	if err != nil {
		fmt.Printf("Failed to get threads for user %s: %v\n", userID, err)
		utils.InternalErrorResponse(c, err)
		return
	}

	// Build response with participants for each thread
	var response []types.ThreadWithParticipants
	for _, thread := range threads {
		participants, err := database.GetThreadParticipants(c, thread.ThreadID)
		if err != nil {
			fmt.Printf("Failed to get participants for thread %s: %v\n", thread.ThreadID, err)
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
func GetMessages(c *gin.Context) {
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
		fmt.Printf("Failed to get messages for thread %s, user %s: %v\n", threadID, userID, err)
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
			fmt.Printf("Error marshaling message: %v\n", err)
			continue
		}

		room.mutex.Lock()
		for conn := range room.clients {
			err := conn.WriteMessage(gorillaws.TextMessage, msgBytes)
			if err != nil {
				fmt.Printf("Error sending message to client: %v\n", err)
				conn.Close()
				delete(room.clients, conn)
			}
		}
		room.mutex.Unlock()
	}
}

// WSHandler handles WebSocket connections for a specific thread
func WSHandler(c *gin.Context) {
	ctx := c.Request.Context()
	threadID := c.Param("threadid")

	// Get authenticated user
	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		fmt.Println("WebSocket error: missing user_id")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	userIDStr := userID.(string)

	// Verify user is a participant of this thread
	err := database.VerifyThreadParticipant(c, threadID, userIDStr)
	if err != nil {
		if err.Error() == "access denied: user is not a participant of this thread" {
			fmt.Printf("WebSocket access denied: user %s not participant of thread %s\n", userIDStr, threadID)
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
		fmt.Printf("WebSocket error checking thread access: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	// Upgrade connection using shared upgrader
	conn, err := websocket.UpgradeConnection(c)
	if err != nil {
		fmt.Printf("WebSocket upgrade error: %v\n", err)
		return
	}
	defer conn.Close()

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

	fmt.Printf("WebSocket connected: user %s joined thread %s\n", userIDStr, threadID)
	log.Info(ctx, fmt.Sprintf("WebSocket connected: user %s joined thread %s", userIDStr, threadID))

	// Read messages from client
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("WebSocket read error (user %s, thread %s): %v\n", userIDStr, threadID, err)
			room.mutex.Lock()
			delete(room.clients, conn)
			room.mutex.Unlock()
			break
		}

		// Parse incoming message
		var inMsg IncomingMessage
		if err := json.Unmarshal(msgBytes, &inMsg); err != nil {
			fmt.Printf("Error parsing message from user %s: %v\n", userIDStr, err)
			// Send error back to client
			errMsg := map[string]string{"error": "invalid message format"}
			errBytes, _ := json.Marshal(errMsg)
			conn.WriteMessage(gorillaws.TextMessage, errBytes)
			continue
		}

		// Validate content is not empty
		if inMsg.Content == "" {
			fmt.Printf("Empty message content from user %s\n", userIDStr)
			errMsg := map[string]string{"error": "message content cannot be empty"}
			errBytes, _ := json.Marshal(errMsg)
			conn.WriteMessage(gorillaws.TextMessage, errBytes)
			continue
		}

		// Save message to database
		savedMessage, err := database.CreateMessage(c, threadID, userIDStr, inMsg.Content)
		if err != nil {
			fmt.Printf("Error saving message to database (user %s, thread %s): %v\n", userIDStr, threadID, err)
			errMsg := map[string]string{"error": "failed to save message"}
			errBytes, _ := json.Marshal(errMsg)
			conn.WriteMessage(gorillaws.TextMessage, errBytes)
			continue
		}

		// Broadcast persisted message to all clients in this thread room
		fmt.Printf("Broadcasting saved message %s in thread %s from user %s\n", savedMessage.MessageID, threadID, userIDStr)
		room.broadcast <- &savedMessage

		// Send push notifications to other thread participants
		go sendMessageNotifications(threadID, userIDStr, savedMessage.MessageID, inMsg.Content)
	}

	fmt.Printf("WebSocket disconnected: user %s left thread %s\n", userIDStr, threadID)
	log.Info(ctx, fmt.Sprintf("WebSocket disconnected: user %s left thread %s", userIDStr, threadID))
}

func GetTotalUnreadCount(c *gin.Context) {

	userID, exists := c.Get("user_id")
	if !exists || userID == "" {
		return
	}

	totalUnreadCount, _ := database.CalculateTotalUnreadCount(c, userID.(string))

	utils.DataResponse(c, http.StatusOK, totalUnreadCount)
}

// GetMessageById returns a single message by its ID
func GetMessageById(c *gin.Context) {
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
		fmt.Printf("Failed to get message %s for user %s: %v\n", messageID, userID, err)
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
		fmt.Printf("Failed to get thread participants for notifications: %v\n", err)
		return
	}

	// Get sender's name
	sender, err := database.GetUserByID(senderID)
	if err != nil {
		fmt.Printf("Failed to get sender info for notification: %v\n", err)
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

		utils.SendPushNotification(
			participant.UserID,
			"message_new",
			senderName,
			displayContent,
			map[string]any{
				"thread_id":  threadID,
				"message_id": messageID,
			},
		)
	}
}
