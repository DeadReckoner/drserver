package websocket

import (
	"context"
	"dr/server/log"
	"dr/server/types"
	"sync"

	"github.com/gorilla/websocket"
)

type ConnectionManager struct {
	connections map[string][]*websocket.Conn
	mutex       sync.RWMutex
}

var Manager = &ConnectionManager{
	connections: make(map[string][]*websocket.Conn),
}

func (m *ConnectionManager) AddConnection(ctx context.Context, jobID string, conn *websocket.Conn) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.connections[jobID] == nil {
		m.connections[jobID] = []*websocket.Conn{}
	}
	m.connections[jobID] = append(m.connections[jobID], conn)
	log.Info(ctx, "Websocket connection added", jobID)
}

func (m *ConnectionManager) RemoveConnection(ctx context.Context, jobID string, conn *websocket.Conn) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	//why not use delete()
	connections := m.connections[jobID]
	for i, c := range connections {
		if c == conn {
			m.connections[jobID] = append(connections[:i], connections[i+1:]...)
			break
		}
	}

	if len(m.connections[jobID]) == 0 {
		delete(m.connections, jobID)
	}
	log.Info(ctx, "Websocket connection removed", jobID)
}

func (m *ConnectionManager) BroadcastToJob(ctx context.Context, jobID string, update types.ProgressUpdate) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	connections := m.connections[jobID]
	for _, conn := range connections {
		// Non-blocking send (don't wait for slow clients)
		go func(c *websocket.Conn) {
			if err := c.WriteJSON(update); err != nil {
				log.Info(ctx, "Error broadcasting job", jobID, update)
				// Connection likely dead, will be cleaned up on next read
			}
		}(conn)
	}
}
