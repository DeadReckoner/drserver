package types

type User struct {
	ID int `json:"user_id"`
	First string `json:"first_name"`
	Last string `json:"last_name"`
	Email string `json:"email"`
}

type Task struct{
	Title string `json:"title"`
	Description string `json:"description"`
	StartDate string `json:"start_date"`
	EndDate string `json:"end_date"`
	Complete bool `json:"complete"`
}

type Roadmap struct{
	Title string `json:"title"`
	Description string `json:"description"`
	StartDate string `json:"start_date"`
	EndDate string `json:"end_date"`
	Complete bool `json:"complete"`
	Tasks []Task `json:"tasks"`
}
