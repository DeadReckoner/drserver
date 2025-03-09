package types

type User struct {
	ID int `json:"user_id"`
	User string `json:"user_name"`
	First string `json:"first_name"`
	Last string `json:"last_name"`
	Email string `json:"email"`
	Friends []User `json:"friends"`
	Tasks []Task `json:"tasks"`
	Roadmaps []Roadmap `json:"road_maps"`
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
