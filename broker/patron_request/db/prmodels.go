package pr_db

import (
	"strings"
	"time"
)

type PatronRequestState string
type PatronRequestSide string
type PatronRequestAction string
type NotificationReceipt string

const (
	NotificationAccepted NotificationReceipt = "ACCEPTED"
	NotificationRejected NotificationReceipt = "REJECTED"
	NotificationSeen     NotificationReceipt = "SEEN"
)

type CustomTime time.Time

const customTimeLayout = "2006-01-02T15:04:05.999999"

var postgresTimeLayouts = []string{
	"2006-01-02T15:04:05.999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05",
}

func (ct CustomTime) MarshalJSON() ([]byte, error) {
	t := time.Time(ct)
	return []byte(`"` + t.Format(customTimeLayout) + `"`), nil
}

func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	var t time.Time
	var err error
	for _, layout := range postgresTimeLayouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			*ct = CustomTime(t)
			return nil
		}
	}
	return err
}

type PrItem struct {
	ID         string     `json:"id"`
	Barcode    string     `json:"barcode"`
	CallNumber *string    `json:"call_number"`
	Title      *string    `json:"title"`
	ItemID     *string    `json:"item_id"`
	CreatedAt  CustomTime `json:"created_at"`
}
