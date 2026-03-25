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

func (ct CustomTime) MarshalJSON() ([]byte, error) {
	t := time.Time(ct)
	return []byte(`"` + t.Format(customTimeLayout) + `"`), nil
}

func (ct *CustomTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	t, err := time.Parse(customTimeLayout, s)
	if err != nil {
		return err
	}
	*ct = CustomTime(t)
	return nil
}

type PrItem struct {
	ID         string     `json:"id"`
	Barcode    string     `json:"barcode"`
	CallNumber *string    `json:"call_number"`
	Title      *string    `json:"title"`
	ItemID     *string    `json:"item_id"`
	CreatedAt  CustomTime `json:"created_at"`
}
