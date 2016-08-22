package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/RuniVN/messenger/model"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/paked/configure"
	"github.com/paked/messenger"
	uuid "github.com/satori/go.uuid"
)

var (
	conf        = configure.New()
	verifyToken = conf.String("verify-token", "delivrto", "The token used to verify facebook")
	verify      = conf.Bool("should-verify", false, "Whether or not the app should verify itself")
	pageToken   = conf.String("page-token", os.Getenv("DELIVR_ACCESS_TOKEN"), "The token that is used to verify the page on facebook")
)

var db *gorm.DB
var err error

func main() {
	conf.Use(configure.NewFlag())
	conf.Use(configure.NewEnvironment())
	conf.Use(configure.NewJSONFromFile("config.json"))

	conf.Parse()
	handleDatabaseStuff()

	// Create a new messenger client
	client := messenger.New(messenger.Options{
		Verify:      *verify,
		VerifyToken: *verifyToken,
		Token:       *pageToken,
	})

	// Setup a handler to be triggered when a message is delivered
	client.HandlePostBack(func(d messenger.PostBack, r *messenger.Response) {

		var userSession model.UserSession
		err = db.Where("fid = ?", d.Sender.ID).First(&userSession).Error
		if err != nil {
			fmt.Println("Cannot get user session")
			return
		}

		switch d.Payload {
		case "Buy":
			err = r.Text("Xin bạn vui lòng paste link vào đây nhé")
			if err != nil {
				fmt.Println("Cannot send to recipient")
			}
		case "Search":
		case "Cancel":
		case "Yes":
			userSession.Status = model.StatusGreeting
			err = db.Save(userSession).Error
			if err != nil {
				fmt.Println("Cannot save user session")
				return
			}

			r.Text("Bạn vui lòng nhập thêm link vào đây")
		case "No":
			userSession.Status = model.StatusGetEmail
			err = db.Save(userSession).Error
			if err != nil {
				fmt.Println("Cannot save user session")
				return
			}
			r.Text("Vui lòng cho chúng tôi xin email của bạn")
		}
	})

	// Setup a handler to be triggered when a message is received
	client.HandleMessage(func(m messenger.Message, r *messenger.Response) {
		fmt.Printf("%v (Sent, %v)\n", m.Text, m.Time.Format(time.UnixDate))

		p, err := client.ProfileByID(m.Sender.ID)
		if err != nil {
			fmt.Println("Something went wrong!", err)
		}
		if !checkUserInSession(m.Sender.ID) {
			var buttonTemplate []messenger.StructuredMessageButton
			buttonBuy := messenger.StructuredMessageButton{Type: "postback", URL: "", Title: "Mua hàng", Payload: "Buy"}
			buttonSearch := messenger.StructuredMessageButton{Type: "postback", URL: "", Title: "Tra cứu đơn hàng", Payload: "Search"}
			buttonCancel := messenger.StructuredMessageButton{Type: "postback", URL: "", Title: "Hủy mua hàng", Payload: "Cancel"}
			buttonTemplate = append(buttonTemplate, buttonBuy)
			buttonTemplate = append(buttonTemplate, buttonCancel)
			buttonTemplate = append(buttonTemplate, buttonSearch)

			err = r.ButtonTemplate("Chào bạn, đây là delivr.to, bạn muốn làm gì?", &buttonTemplate)
			if err != nil {
				fmt.Println("Cannot send to recipient")
				return
			}
			if !checkUserExist(m.Sender.ID) {
				var user model.FUser
				user.FID = m.Sender.ID
				user.Firstname = p.FirstName
				user.Lastname = p.LastName

				err = db.Create(&user).Error
				if err != nil {
					fmt.Println("Cannot create user")
					return
				}
			}
			var userSession model.UserSession
			userSession.FID = m.Sender.ID
			userSession.Status = model.StatusGreeting
			userSession.IsActive = true

			err = db.Create(&userSession).Error
			if err != nil {
				fmt.Println("Cannot create user session")
				return
			}

		} else {
			var userSession model.UserSession
			err = db.Where("fid = ?", m.Sender.ID).First(&userSession).Error
			if err != nil {
				fmt.Println("Cannot get user session")
				return
			}

			switch userSession.Status {
			case model.StatusGreeting:
				if isValidLink(m.Text) {
					userSession.Status = model.StatusGetLink
					userSession.Link = m.Text
					err = db.Save(userSession).Error
					if err != nil {
						fmt.Println("Cannot save user session")
						return
					}
					r.Text("Bạn muốn số lượng bao nhiêu?")
				} else {
					r.Text("Xin lỗi, hình như chúng tôi không thể nhận ra link này")
				}

			case model.StatusGetLink:
				if isValidQuantity(m.Text) {
					var order model.Order
					order.FID = m.Sender.ID
					order.UUID = uuid.NewV4().String()
					err = db.Create(&order).Error
					if err != nil {
						fmt.Println("Cannot create order")
						return
					}

					var item model.Item
					item.Link = userSession.Link
					item.Quantity, _ = strconv.ParseFloat(m.Text, 64)
					item.OrderID = order.ID
					item.IsActive = true
					err = db.Create(&item).Error
					if err != nil {
						fmt.Println("Cannot create item")
						return
					}

					userSession.Status = model.StatusGetQuantity
					userSession.Link = ""
					userSession.Quantity = 0
					err = db.Save(userSession).Error
					if err != nil {
						fmt.Println("Cannot save user session")
						return
					}

					buttonYes := messenger.StructuredMessageButton{Type: "postback", URL: "", Title: "Có", Payload: "Yes"}
					buttonNo := messenger.StructuredMessageButton{Type: "postback", URL: "", Title: "Không", Payload: "No"}
					var buttonTemplate []messenger.StructuredMessageButton
					buttonTemplate = append(buttonTemplate, buttonYes)
					buttonTemplate = append(buttonTemplate, buttonNo)

					err = r.ButtonTemplate("Bạn còn muốn order thêm sản phẩm nào không?", &buttonTemplate)
					if err != nil {
						fmt.Println("Cannot send button")
					}
				} else {
					r.Text("Bạn vui lòng kiểm tra lại số lượng nhé")
				}

			case model.StatusGetEmail:
				if isValidEmail(m.Text) {
					userSession.Status = model.StatusGetPhone
					err = db.Save(userSession).Error
					if err != nil {
						fmt.Println("Cannot save user session")
						return
					}
					err = db.Model(&model.Order{}).Where("fid = ?", m.Sender.ID).Update("email", m.Text).Error
					if err != nil {
						fmt.Println("Cannot update email for order")
					}

					r.Text("Bạn cho mình xin số điện thoại bạn nhé")
				} else {
					r.Text("Hình như nó không phải email bạn ơi")
				}
			case model.StatusGetPhone:
				if isValidPhone(m.Text) {
					userSession.Status = model.StatusGoodbye
					err = db.Save(userSession).Error
					if err != nil {
						fmt.Println("Cannot save user session")
						return
					}
					err = db.Model(&model.Order{}).Where("fid = ?", m.Sender.ID).Update("phone", m.Text).Error
					if err != nil {
						fmt.Println("Cannot update phone for order")
					}
					r.Text("Xong rồi. Mình đã ghi chú lại đơn hàng của bạn. Bạn đợi tin nhé!")
				} else {
					r.Text("Bạn nhập đúng số phone giùm mình nha")
				}

			case model.StatusGoodbye:

			}

		}

	})

	// // Setup a handler to be triggered when a message is read
	// client.HandleRead(func(m messenger.Read, r *messenger.Response) {
	// fmt.Println("Read at:", m.Watermark().Format(time.UnixDate))
	/* }) */

	fmt.Println("Serving messenger bot on localhost:8080")

	http.ListenAndServe("localhost:8080", client.Handler())
}

func handleDatabaseStuff() {
	db, err = gorm.Open("postgres", "postgres://postgres:qq123123@dockerhost:5432/delivr-bot?sslmode=disable&connect_timeout=10")
	if err != nil {
		panic(err)
	}

	if !db.HasTable(&model.FUser{}) {
		db.CreateTable(&model.FUser{})
	}

	if !db.HasTable(&model.Order{}) {
		db.CreateTable(&model.Order{})
	}

	if !db.HasTable(&model.UserSession{}) {
		db.CreateTable(&model.UserSession{})
	}

	if !db.HasTable(&model.Item{}) {
		db.CreateTable(&model.Item{})
	}
}

func checkUserExist(fid int64) bool {
	var count int
	err := db.Model(&model.FUser{}).Where("fid = ?", fid).Count(&count).Error
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	if count > 0 {
		return true
	}
	return false
}
func checkUserInSession(fid int64) bool {
	var count int
	err := db.Model(&model.UserSession{}).Where("fid = ? AND is_active = ?", strconv.Itoa(int(fid)), "true").Count(&count).Error
	if err != nil {
		fmt.Println(err.Error())
		return false
	}
	if count > 0 {
		return true
	}
	return false
}
