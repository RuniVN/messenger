package model

// Order stores data for an order
type Order struct {
	ID    int    `json:"id"`
	FID   int64  `json:"fid" gorm:"column:fid"`
	UUID  string `json:"uuid"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}
