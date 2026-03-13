package domain

type Menu struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Price   int64  `json:"price"`
	CategoryID *int `json:"category_id,omitempty"`
}
