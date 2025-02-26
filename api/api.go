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
  

  router.POST("/task", createTask)
  router.GET("/tasks/:userid", getTasks)
  router.GET("/task/:taskid", todo)
  router.PATCH("/task/:taskid", todo)
  router.DELETE("/task/:taskid", todo)

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


func createTask(c *gin.Context){
  log.Println("Created Task")
}

func getTasks(c *gin.Context){
  userid := c.Param("id")

  log.Println("Get tasks for user", userid)
}


func todo(c *gin.Context){
  log.Println("ToDo")
}


