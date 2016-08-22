package model

import "time"

// UserSession stores data for a session of user
type UserSession struct {
	ID       int       `json:"id"`
	FID      int64     `json:"fid" gorm:"column:fid"`
	Status   int       `json:"status"`
	Link     string    `json:"link"`
	Quantity float64   `json:"quantity"`
	IsActive bool      `json:is_active`
	Time     time.Time `json:"time"`
}
