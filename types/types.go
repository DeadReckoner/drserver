package types

type User struct {
	ID int `json:"user_id"`
	User string `json:"user_name"`
	First string `json:"first_name"`
	Last string `json:"last_name"`
}
