package model

// FUser stores data for facebook user
type FUser struct {
	ID        int    `json:"id"`
	FID       int64  `json:"fid" gorm:"column:fid"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
}
