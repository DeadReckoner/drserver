package handlers

import (
	"dr/server/types"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var clients = make(map[*websocket.Conn]string) //Connected clients
var broadcast = make(chan []byte)
var mutex = &sync.Mutex{}

func WSHandler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	user_id, exists := c.Get("user_id")
	if !exists || user_id == "" {
		//errors.New("missing or invalid user_id")
		return
	}

	go handleMessage(user_id.(string))

	mutex.Lock()
	clients[conn] = user_id.(string)
	mutex.Unlock()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			mutex.Lock()
			delete(clients, conn)
			mutex.Unlock()
			break
		}
		broadcast <- message
	}

}

func handleMessage(user_id string) {
	for {
		message := <-broadcast

		mutex.Lock()
		for client, clientID := range clients {
			if clientID == user_id {
				continue
			} else {
				err := client.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					client.Close()
					delete(clients, client)
				}
			}
		}
		mutex.Unlock()
	}
}

func handleConnection(conn *websocket.Conn) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Error reading message:", err)
			break
		}
		fmt.Printf("recieved: %s\n", message)

		if err = conn.WriteMessage(websocket.TextMessage, message); err != nil {
			fmt.Println("error writing message:", err)
			break
		}
	}
}

func CreateThread(c *gin.Context) {
	userID := c.GetString("user_id")
	var newThread types.Thread

	fmt.Printf("Creating new thread: %+v\n", userID)

	c.JSON(http.StatusCreated, newThread)
}
