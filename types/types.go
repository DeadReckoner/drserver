package types

type User struct {
	ID    string
	First string `json:"first_name"`
	Last  string `json:"last_name"`
	Email string `json:"email"`
}

type Task struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Level       string `json:level`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Completed   bool   `json:"completed"`
}
