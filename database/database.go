package database

import (
	"context"
	"database/sql"
	"dr/server/types"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
)

var db *sql.DB


func Database() {
	conn, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to the database: %v", err)
	}
	defer conn.Close(context.Background())

	// Example query to test connection
	var version string
	if err := conn.QueryRow(context.Background(), "SELECT version()").Scan(&version); err != nil {
		log.Fatalf("Query failed: %v", err)
	}

	log.Println("Connected to:", version)

}


func GetUser(id string) string {
	return getUser(db, id)
}

func getUser(db *sql.DB, id string) string {
	
	id_int, err := strconv.Atoi(id)

	if err != nil { 
		panic(err)
	}

	result, err := db.Query("SELECT * FROM users WHERE user_id = ?", id_int)
	
	if err != nil { 
		panic(err)
	}


	var user_id int
	var user string
	var first string
	var last string


	if result.Next(){
		err = result.Scan(&user_id, &user, &first, &last)
	} else {
		fmt.Println("Nothing found here ...")
	}


	if err != nil {
		panic(err)
	}

	fmt.Printf("Id: %d User: %s First: %s Last: %s\n", user_id, user, first, last)
	
	return user
}

func GetUserByName(name string) string {
	return getUserByName(db, name)
}


func getUserByName(db *sql.DB, name string) string{
	result, err := db.Query("SELECT * FROM users WHERE first_name = ?", name)

	if err != nil{
		panic(err)
	}
	var user_id int
	var user string
	var first string
	var last string

	if result.Next(){
		err = result.Scan(&user_id, &user, &first, &last)
	} else {
		fmt.Println("Nothing found here ...")
	}
	if err != nil {
		panic(err)
	}

	fmt.Printf("Id: %d User: %s First: %s Last: %s\n", user_id, user, first, last)
	
	return user

}

func CreateUser (user types.User){

	createUser(db, user)
}


func createUser(db *sql.DB, user types.User){

		stmt, err := db.Prepare("INSERT INTO users VALUES(?, ?, ?, ?)")
		randomNumber := rand.Intn(1000000)

		if err != nil {
			panic(err)
		}

		res, err := stmt.Exec(randomNumber, user.User, user.First, user.Last)
		
		if err != nil {
			panic(err)
		}

		lastId, err := res.LastInsertId()
		if err != nil {
			panic(err)
		}
		rowCnt, err := res.RowsAffected()
		if err != nil {
			panic(err)
		}

		fmt.Printf("ID = %d, Row = %d\n", lastId, rowCnt)
	}


func DeleteUser(id string){
	deleteUser(db, id)
}

func deleteUser(db *sql.DB, id string){
	stmt, err := db.Prepare("DELETE FROM users WHERE user_id = ?")

	if err != nil{
		panic(err)
	}

	res, err := stmt.Exec(id)

	if err != nil{
		panic(err)
	}

	lastId, err := res.LastInsertId()
	if err != nil {
		panic(err)
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		panic(err)
	}

	fmt.Printf("ID = %d, Row = %d\n", lastId, rowCnt)
	

}
