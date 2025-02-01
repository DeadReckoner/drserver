package api

import (
	"log"

	"github.com/gin-gonic/gin"
)


func Server(){
	router := gin.Default()

	router.POST("/user", createUser)
	router.GET("/user/:id", getUser)
	router.PATCH("/user/:id", updateUser)
	router.DELETE("/user/:id", deleteUser)

	router.Run("localhost:8080")
}

func createUser(c *gin.Context){
	// user := database.GetUser(c.Param("id"))

	// c.IndentedJSON(http.StatusOK, user)
	log.Println("Create")
}
func updateUser(c *gin.Context){

	log.Println("Update")
}

func getUser(c *gin.Context){

	log.Println("Get")
}

func deleteUser(c *gin.Context){
	log.Println("Delete")
}