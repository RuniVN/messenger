package model

// Item stores data for an item
type Item struct {
	ID       int     `json:"id"`
	Link     string  `json:"link"`
	Quantity float64 `json:"quantity"`
	OrderID  int     `json:"oder_id"`
	IsActive bool    `json:"is_active"`
}
