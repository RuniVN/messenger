package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/RuniVN/messenger"
	"github.com/RuniVN/messenger/model"
	"github.com/carlescere/scheduler"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/paked/configure"
)

var (
	conf        = configure.New()
	verifyToken = conf.String("verify-token", os.Getenv("DELIVR_VERIFY_TOKEN"), "The token used to verify facebook")
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
			err = r.Text("Xin bạn cho biết mã đơn hàng")
			if err != nil {
				fmt.Println("Cannot send to recipient")
			}

			userSession.Status = model.StatusCheckOrder
			err = db.Save(userSession).Error
			if err != nil {
				fmt.Sprintln("Cannot save user session %s", err.Error())
				handleError(r)
				return
			}
		case "Cancel":
			err = r.Text("Bạn đã chọn hủy đơn hàng. Xin cho biết mã đơn hàng.")
			if err != nil {
				fmt.Println("Cannot send to recipient")
			}

			userSession.Status = model.StatusCancelOrder
			err = db.Save(userSession).Error
			if err != nil {
				fmt.Sprintln("Cannot save user session %s", err.Error())
				handleError(r)
				return
			}
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
			userSession.Time = time.Now()

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
					userSession.Status = model.StatusGetAddress
					err = db.Save(userSession).Error
					if err != nil {
						fmt.Println("Cannot save user session")
						return
					}
					err = db.Model(&model.Order{}).Where("fid = ?", m.Sender.ID).Update("email", m.Text).Error
					if err != nil {
						fmt.Println("Cannot update email for order")
					}

					r.Text("Bạn cho mình địa chỉ mà bạn muốn nhận hàng.")
				} else {
					r.Text("Hình như nó không phải email bạn ơi")
				}
			case model.StatusGetAddress:
				userSession.Status = model.StatusGetNote
				err = db.Save(userSession).Error
				if err != nil {
					fmt.Println("Cannot save user session")
					return
				}
				err = db.Model(&model.Order{}).Where("fid = ?", m.Sender.ID).Update("address", m.Text).Error
				if err != nil {
					fmt.Println("Cannot update address for order")
				}

				r.Text("Bạn có điều gì cần lưu ý trong đơn hàng này không?")
			case model.StatusGetNote:
				userSession.Status = model.StatusGetPhone
				err = db.Save(userSession).Error
				if err != nil {
					fmt.Println("Cannot save user session")
					return
				}
				err = db.Model(&model.Order{}).Where("fid = ?", m.Sender.ID).Update("note", m.Text).Error
				if err != nil {
					fmt.Println("Cannot update email for order")
				}

				r.Text("Bạn cho mình xin số điện thoại bạn nhé")
			case model.StatusGetPhone:
				if isValidPhone(m.Text) {
					userSession.Status = model.StatusGoodbye
					err = db.Save(userSession).Error
					if err != nil {
						fmt.Sprintln("Cannot save user session %s", err.Error())
						return
					}

					var order model.Order
					err = db.Where("fid = ?", m.Sender.ID).First(&order).Error
					if err != nil {
						fmt.Sprintln("Cannot get order %v", err.Error())
						handleError(r)
						return
					}

					orderCode, err := generateHash()
					if err != nil {
						fmt.Sprintln("Cannot gen hashID %v", err.Error())
						handleError(r)
						return
					}
					var items []model.Item
					err = db.Where("order_id = ?", order.ID).Find(&items).Error
					if err != nil {
						fmt.Sprintln("Cannot get item %v", err.Error())
						handleError(r)
						return
					}
					var orderItems = make([]map[string]interface{}, 0)
					for _, v := range items {
						orderItem := map[string]interface{}{
							"link":     v.Link,
							"quantity": v.Quantity,
						}
						orderItems = append(orderItems, orderItem)
					}

					orderMap := map[string]interface{}{
						"name":        p.FirstName + " " + p.LastName,
						"fid":         strconv.Itoa(int(m.Sender.ID)),
						"phone":       m.Text,
						"email":       order.Email,
						"order_code":  orderCode,
						"order_items": orderItems,
						"note":        order.Note,
						"address":     order.Address,
					}

					body, err := json.Marshal(&orderMap)
					if err != nil {
						fmt.Sprintln("Cannot marshal order %s", err.Error())
						handleError(r)
						return
					}

					req, err := http.NewRequest("POST", "http://localhost:8008/api/bot/orders", bytes.NewBuffer(body))
					if err != nil {
						fmt.Sprintln("Cannot create request to create order %v", err.Error())
						handleError(r)
						return
					}

					req.Header.Set("Content-Type", "application/json")

					client := &http.Client{}
					resp, err := client.Do(req)
					if err != nil {
						fmt.Sprintln("Cannot do request %v", err.Error())
						handleError(r)
						return
					}
					defer resp.Body.Close()

					if resp.StatusCode == 200 {
						r.Text("Xong rồi. Mình đã ghi chú lại đơn hàng của bạn. Đơn hàng của bạn có mã là: " + orderCode + ". Sẽ có nhân viên của chúng tôi liên lạc với bạn. Bạn đợi tin nhé!")
						err = db.Delete(&order).Error
						if err != nil {
							fmt.Sprintln("Cannot delete order, %v", err.Error())

						}

					} else {
						fmt.Sprintln("Cannot create order, response status: %d", resp.Status)
						handleError(r)
						return
					}

				} else {
					r.Text("Bạn nhập đúng số phone giùm mình nha")
				}

			case model.StatusGoodbye:
				userSession.Status = model.StatusGreeting
				err = db.Save(userSession).Error
				if err != nil {
					fmt.Println("Cannot save user session")
					return
				}

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
			case model.StatusCheckOrder:
				req, err := http.NewRequest("GET", "http://localhost:8008/api/bot/orders?order_code="+m.Text, nil)
				if err != nil {
					fmt.Sprintln("Cannot create request to check order %s", err.Error())
					handleError(r)
					return
				}

				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					fmt.Sprintln("Cannot do request %s", err.Error())
					handleError(r)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != 200 {
					if resp.StatusCode == 500 {
						r.Text("Xin lỗi, chúng tôi không thể tìm thấy thông tin về đơn hàng này.")
						return
					} else {
						fmt.Sprintln("Cannot check order, response status: %d", resp.Status)
						handleError(r)
						return
					}
				}

				type Response struct {
					OrderStatus string `json:"order_status"`
				}
				var response Response

				err = json.NewDecoder(resp.Body).Decode(&response)
				if err != nil {
					fmt.Sprintln("Cannot decode response %s", err.Error())
					handleError(r)
					return
				}
				r.Text("Chào bạn, tình trạng đơn hàng của bạn là: " + response.OrderStatus)

				userSession.Status = model.StatusGoodbye
				err = db.Save(userSession).Error
				if err != nil {
					fmt.Sprintln("Cannot save user session %s", err.Error())
					return
				}
			case model.StatusCancelOrder:
				req, err := http.NewRequest("DELETE", "http://localhost:8008/api/bot/orders?order_code="+m.Text, nil)
				if err != nil {
					fmt.Sprintln("Cannot create request to delete order %s", err.Error())
					handleError(r)
					return
				}

				req.Header.Set("Content-Type", "application/json")

				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					fmt.Sprintln("Cannot do request %s", err.Error())
					handleError(r)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != 200 {
					if resp.StatusCode == 500 {
						r.Text("Xin lỗi, chúng tôi không thể tìm thấy thông tin về đơn hàng này.")
						return
					} else {
						fmt.Sprintln("Cannot check order, response status: %d", resp.Status)
						handleError(r)
						return
					}
				}

				r.Text("Đơn hàng của bạn đã được hủy thành công.")
				userSession.Status = model.StatusGoodbye
				err = db.Save(userSession).Error
				if err != nil {
					fmt.Sprintln("Cannot save user session %s", err.Error())
					return
				}
			}

		}

	})

	// // Setup a handler to be triggered when a message is read
	// client.HandleRead(func(m messenger.Read, r *messenger.Response) {
	// fmt.Println("Read at:", m.Watermark().Format(time.UnixDate))
	/* }) */

	fmt.Println("Serving messenger bot on localhost:8080")

	http.ListenAndServe("localhost:8080", client.Handler())

	scheduler.Every().Day().At("00:01").Run(jobClearSession)
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

func jobClearSession() {
	err = db.Model(&model.UserSession{}).Where("time < ?", time.Now().AddDate(0, 0, -1)).Update("is_active", false).Error
	if err != nil {
		fmt.Println("Cannot update user session")
	}
}

func handleError(r *messenger.Response) {
	r.Text("Xin lỗi, hiện tại delivr không thể giải quyết đơn hàng của bạn. ")
}
