package handlers

import (
	"dr/server/api/utils"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func WSHandler(c *gin.Context) {
	threadID := c.Param("threadid")
	userID := c.GetString("user_id")
	log.Printf("WebSocket connect thread=%s user=%s\n", threadID, userID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v\n", err)
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to upgrade to WebSocket")
		return
	}
	defer conn.Close()

	conn.SetReadLimit(1024 * 64)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	var writeMu sync.Mutex

	for {
		mt, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v\n", err)
			}
			break
		}
		log.Printf("Received (thread=%s user=%s): %s\n", threadID, userID, message)

		writeMu.Lock()
		err = conn.WriteMessage(mt, message)
		writeMu.Unlock()
		if err != nil {
			log.Printf("WebSocket write error: %v\n", err)
			break
		}
	}
}
