package dto

type UpsertAnnouncementRequest struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Content  string  `json:"content"`
	Level    string  `json:"level"`
	Status   string  `json:"status"`
	Audience string  `json:"audience"`
	Sticky   bool    `json:"sticky"`
	StartsAt *string `json:"startsAt"`
	EndsAt   *string `json:"endsAt"`
}
