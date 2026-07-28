package websocket

// In-package tests for the WebSocket hub. Tests build fresh ConnectionManager
// instances (never mutating the package-level Manager) so internal map state
// can be asserted directly. End-to-end broadcast tests run a real gin route
// through httptest and dial it with the gorilla client; client-side read
// deadlines keep assertions deterministic despite BroadcastToJob writing from
// spawned goroutines.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dr/server/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// infraUpgradeResult captures what UpgradeConnection returned to the handler.
type infraUpgradeResult struct {
	conn *websocket.Conn
	err  error
}

// infraStartWSServer runs a gin route that upgrades incoming requests and
// reports each UpgradeConnection outcome on the returned channel.
func infraStartWSServer(t *testing.T) (*httptest.Server, chan infraUpgradeResult) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	results := make(chan infraUpgradeResult, 16)
	r := gin.New()
	r.GET("/ws", func(c *gin.Context) {
		conn, err := UpgradeConnection(c)
		results <- infraUpgradeResult{conn: conn, err: err}
	})
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts, results
}

// infraDialWS connects a gorilla client to the test server's /ws route.
func infraDialWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// infraRecvUpgrade waits (bounded) for the handler-side upgrade result.
func infraRecvUpgrade(t *testing.T, ch chan infraUpgradeResult) infraUpgradeResult {
	t.Helper()
	select {
	case r := <-ch:
		if r.conn != nil {
			t.Cleanup(func() { _ = r.conn.Close() })
		}
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for upgrade result")
		return infraUpgradeResult{}
	}
}

// infraNewManager builds a fresh hub so tests never touch the global Manager.
func infraNewManager() *ConnectionManager {
	return &ConnectionManager{connections: make(map[string][]*websocket.Conn)}
}

// infraReadUpdate reads one JSON frame with a deadline so a missing broadcast
// fails the read instead of hanging the test.
func infraReadUpdate(conn *websocket.Conn, timeout time.Duration) (types.ProgressUpdate, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var got types.ProgressUpdate
	err := conn.ReadJSON(&got)
	return got, err
}

func TestInfraWSUpgradeConnection_Success(t *testing.T) {
	ts, results := infraStartWSServer(t)

	client := infraDialWS(t, ts)
	require.NotNil(t, client)

	res := infraRecvUpgrade(t, results)
	assert.NoError(t, res.err)
	assert.NotNil(t, res.conn)
}

func TestInfraWSUpgradeConnection_PlainHTTPRejected(t *testing.T) {
	ts, results := infraStartWSServer(t)

	// A plain GET without the Upgrade handshake headers must fail.
	resp, err := http.Get(ts.URL + "/ws")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	res := infraRecvUpgrade(t, results)
	assert.Error(t, res.err)
	assert.Nil(t, res.conn)
}

func TestInfraWSAddConnection(t *testing.T) {
	m := infraNewManager()
	ctx := context.Background()
	// Dummy conns: Add/Remove only compare pointers and never write frames.
	connA, connB, connC := &websocket.Conn{}, &websocket.Conn{}, &websocket.Conn{}

	m.AddConnection(ctx, "job-1", connA)
	require.Len(t, m.connections["job-1"], 1)

	m.AddConnection(ctx, "job-1", connB)
	require.Len(t, m.connections["job-1"], 2)
	assert.Same(t, connA, m.connections["job-1"][0])
	assert.Same(t, connB, m.connections["job-1"][1])

	// A different jobID gets its own slice.
	m.AddConnection(ctx, "job-2", connC)
	assert.Len(t, m.connections["job-2"], 1)
	assert.Len(t, m.connections["job-1"], 2)
}

func TestInfraWSRemoveConnection(t *testing.T) {
	ctx := context.Background()

	t.Run("removing one of two leaves the other", func(t *testing.T) {
		m := infraNewManager()
		connA, connB := &websocket.Conn{}, &websocket.Conn{}
		m.AddConnection(ctx, "job-1", connA)
		m.AddConnection(ctx, "job-1", connB)

		m.RemoveConnection(ctx, "job-1", connA)
		require.Len(t, m.connections["job-1"], 1)
		assert.Same(t, connB, m.connections["job-1"][0])
	})

	t.Run("removing the last conn deletes the jobID key", func(t *testing.T) {
		m := infraNewManager()
		connA := &websocket.Conn{}
		m.AddConnection(ctx, "job-1", connA)

		m.RemoveConnection(ctx, "job-1", connA)
		_, exists := m.connections["job-1"]
		assert.False(t, exists, "empty jobID entry should be deleted")
	})

	t.Run("removing an unknown conn is a no-op", func(t *testing.T) {
		m := infraNewManager()
		connA := &websocket.Conn{}
		m.AddConnection(ctx, "job-1", connA)

		m.RemoveConnection(ctx, "job-1", &websocket.Conn{})
		require.Len(t, m.connections["job-1"], 1)
		assert.Same(t, connA, m.connections["job-1"][0])
	})

	t.Run("removing from an unknown jobID neither panics nor creates the key", func(t *testing.T) {
		m := infraNewManager()
		m.RemoveConnection(ctx, "ghost-job", &websocket.Conn{})
		_, exists := m.connections["ghost-job"]
		assert.False(t, exists)
	})
}

func TestInfraWSBroadcastToJob_DeliversToRegisteredClients(t *testing.T) {
	ts, results := infraStartWSServer(t)
	m := infraNewManager()
	ctx := context.Background()

	// Two clients on job-1, one on job-other.
	client1 := infraDialWS(t, ts)
	server1 := infraRecvUpgrade(t, results)
	require.NoError(t, server1.err)
	client2 := infraDialWS(t, ts)
	server2 := infraRecvUpgrade(t, results)
	require.NoError(t, server2.err)
	clientOther := infraDialWS(t, ts)
	serverOther := infraRecvUpgrade(t, results)
	require.NoError(t, serverOther.err)

	m.AddConnection(ctx, "job-1", server1.conn)
	m.AddConnection(ctx, "job-1", server2.conn)
	m.AddConnection(ctx, "job-other", serverOther.conn)

	update := types.ProgressUpdate{
		Type:     "progress",
		JobID:    "job-1",
		Progress: 42,
		Status:   "processing",
		Message:  "generating milestones",
	}
	m.BroadcastToJob(ctx, "job-1", update)

	got1, err := infraReadUpdate(client1, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, update, got1)

	got2, err := infraReadUpdate(client2, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, update, got2)

	// The client on a different jobID receives nothing: its read deadline
	// expires without a frame.
	_, err = infraReadUpdate(clientOther, 250*time.Millisecond)
	assert.Error(t, err, "client on another jobID must not receive the broadcast")
}

func TestInfraWSBroadcastToJob_NoConnections(t *testing.T) {
	m := infraNewManager()
	// Broadcasting to a jobID nobody is watching must simply return.
	assert.NotPanics(t, func() {
		m.BroadcastToJob(context.Background(), "nobody-home", types.ProgressUpdate{
			Type:  "progress",
			JobID: "nobody-home",
		})
	})
}

func TestInfraWSBroadcastToJob_DeadConnDoesNotBlockLiveConn(t *testing.T) {
	ts, results := infraStartWSServer(t)
	m := infraNewManager()
	ctx := context.Background()

	clientDead := infraDialWS(t, ts)
	serverDead := infraRecvUpgrade(t, results)
	require.NoError(t, serverDead.err)
	clientLive := infraDialWS(t, ts)
	serverLive := infraRecvUpgrade(t, results)
	require.NoError(t, serverLive.err)

	m.AddConnection(ctx, "job-1", serverDead.conn)
	m.AddConnection(ctx, "job-1", serverLive.conn)

	// Kill the first server-side conn; its write will fail and only be logged.
	require.NoError(t, serverDead.conn.Close())
	_ = clientDead.Close()

	update := types.ProgressUpdate{Type: "progress", JobID: "job-1", Progress: 99}
	assert.NotPanics(t, func() {
		m.BroadcastToJob(ctx, "job-1", update)
	})

	got, err := infraReadUpdate(clientLive, 2*time.Second)
	require.NoError(t, err, "live conn must still receive the update")
	assert.Equal(t, update, got)
}

func TestInfraWSManager_ConcurrentAccess(t *testing.T) {
	// Smoke test for the RWMutex usage: concurrent Add/Remove (writers) and
	// Broadcast (reader) on a shared manager must be race-free. Broadcasts
	// target a jobID that never has connections so no frame writes happen on
	// the dummy conns.
	m := infraNewManager()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		jobID := fmt.Sprintf("job-%d", i)

		wg.Add(2)
		go func() {
			defer wg.Done()
			for n := 0; n < 25; n++ {
				conn := &websocket.Conn{}
				m.AddConnection(ctx, jobID, conn)
				m.RemoveConnection(ctx, jobID, conn)
			}
		}()
		go func() {
			defer wg.Done()
			for n := 0; n < 25; n++ {
				m.BroadcastToJob(ctx, "always-empty", types.ProgressUpdate{
					Type:  "progress",
					JobID: "always-empty",
				})
			}
		}()
	}
	wg.Wait()

	// Every add was paired with a remove, so the map must be empty again.
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	assert.Empty(t, m.connections)
}
