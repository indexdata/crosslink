package pr_db

import (
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

type PrItem struct {
	ID         string    `json:"id"`
	Barcode    string    `json:"barcode"`
	CallNumber *string   `json:"call_number"`
	Title      *string   `json:"title"`
	ItemID     *string   `json:"item_id"`
	CreatedAt  time.Time `json:"created_at"`
}
