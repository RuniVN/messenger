package main

import (
	"regexp"
	"strings"
	"time"

	"github.com/asaskevich/govalidator"
	hashids "github.com/speps/go-hashids"
)

func isValidLink(link string) bool {
	return govalidator.IsURL(link)
}

func isValidQuantity(quantity string) bool {
	return govalidator.IsFloat(quantity)
}

func isValidEmail(email string) bool {
	email = strings.ToLower(email)
	return govalidator.IsEmail(email)
}

func isValidPhone(phone string) bool {
	re := regexp.MustCompile(`^[0-9\-\+]{9,15}$`)
	return re.MatchString(phone)
}

func generateHash() (string, error) {
	hd := hashids.NewData()
	hd.Alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	h := hashids.NewWithData(hd)
	timeStamp := time.Now().UnixNano() / int64(time.Millisecond)
	return h.Encode([]int{int(timeStamp)})
}
