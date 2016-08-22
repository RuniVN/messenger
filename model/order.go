package model

import "time"

// Order stores data for an order
type Order struct {
	ID        int       `json:"id"`
	FID       int64     `json:"fid" gorm:"column:fid"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Address   string    `json:"address"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
