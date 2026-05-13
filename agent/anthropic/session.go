package anthropic

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	sessionTTL    = 30 * time.Minute
	maxSessionAge = 2 * time.Hour
)

// Session holds the conversation history for a single agent session
type Session struct {
	ID        string
	Mode      AgentMode
	messages  []anthropic.MessageParam
	CreatedAt time.Time
	UpdatedAt time.Time
	mu        sync.RWMutex
}

// Messages returns a snapshot of the conversation history (thread-safe)
func (s *Session) Messages() []anthropic.MessageParam {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]anthropic.MessageParam, len(s.messages))
	copy(out, s.messages)
	return out
}

// AddUserMessage appends a user text message to the history
func (s *Session) AddUserMessage(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, anthropic.NewUserMessage(
		anthropic.NewTextBlock(text),
	))
	s.UpdatedAt = time.Now()
}

// AddAssistantText appends a simple assistant text response
func (s *Session) AddAssistantText(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, anthropic.NewAssistantMessage(
		anthropic.NewTextBlock(text),
	))
	s.UpdatedAt = time.Now()
}

// AddAssistantMessageRaw appends the full raw content blocks from an assistant turn.
// Used when the response includes tool_use blocks that must be preserved for the tool_result.
func (s *Session) AddAssistantMessageRaw(content []anthropic.ContentBlockUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()

	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(content))
	for _, b := range content {
		switch b.Type {
		case "text":
			blocks = append(blocks, anthropic.NewTextBlock(b.Text))
		case "tool_use":
			blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, b.Input, b.Name))
		}
	}

	s.messages = append(s.messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleAssistant,
		Content: blocks,
	})
	s.UpdatedAt = time.Now()
}

// AddToolResult appends a tool_result message (required after tool_use in the assistant turn)
func (s *Session) AddToolResult(toolUseID string, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resultJSON, _ := json.Marshal(result)

	s.messages = append(s.messages, anthropic.NewUserMessage(
		anthropic.NewToolResultBlock(toolUseID, string(resultJSON), false),
	))
	s.UpdatedAt = time.Now()
}

// MessageCount returns the number of messages in the session
func (s *Session) MessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// SessionStore is an in-memory session store with TTL eviction.
// For production, swap the map for Redis with SETEX.
type SessionStore struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionStore creates a new session store with automatic eviction
func NewSessionStore() *SessionStore {
	ss := &SessionStore{
		sessions: make(map[string]*Session),
	}
	go ss.evictLoop()
	return ss
}

// GetOrCreate retrieves an existing session or creates a new one
func (ss *SessionStore) GetOrCreate(id string, mode AgentMode) (*Session, error) {
	if id == "" {
		return nil, fmt.Errorf("session ID cannot be empty")
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	if s, ok := ss.sessions[id]; ok {
		// Mode switch: reset the session if the mode changed
		if s.Mode != mode {
			s.messages = nil
			s.Mode = mode
			s.UpdatedAt = time.Now()
		}
		return s, nil
	}

	s := &Session{
		ID:        id,
		Mode:      mode,
		messages:  []anthropic.MessageParam{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	ss.sessions[id] = s
	return s, nil
}

// Save updates the session's last-seen time (no-op with in-memory store, used for Redis hook)
func (ss *SessionStore) Save(s *Session) {
	// With Redis, you'd re-serialize and SETEX here
	_ = s
}

// Delete removes a session (call this after roadmap is saved to DB)
func (ss *SessionStore) Delete(id string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.sessions, id)
}

// evictLoop removes expired sessions every 5 minutes
func (ss *SessionStore) evictLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ss.evict()
	}
}

func (ss *SessionStore) evict() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	now := time.Now()
	for id, s := range ss.sessions {
		if now.Sub(s.UpdatedAt) > sessionTTL || now.Sub(s.CreatedAt) > maxSessionAge {
			delete(ss.sessions, id)
		}
	}
}
