package handlers

import (
	"dr/server/log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func CreateFriendRequest(c *gin.Context) {

	log.Info(c, "Created Friend Request")

	c.JSON(http.StatusOK, "Friend Request Sent")
}

func GetFriendRequests(c *gin.Context) {

	log.Info(c, "Getting Friend Requests")

	c.JSON(http.StatusOK, "Friend Requests")
}
