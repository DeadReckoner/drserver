package main

import (
	"dr/server/api"
	"dr/server/database"
)

func main() {

	database.Database()
	api.Server()

}
