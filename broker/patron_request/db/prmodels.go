package pr_db

import (
	"time"
)

type PatronRequestState string
type PatronRequestSide string
type PatronRequestAction string
type NotificationReceipt string
type NotificationDirection string
type NotificationKind string

const (
	NotificationAccepted     NotificationReceipt = "ACCEPTED"
	NotificationRejected     NotificationReceipt = "REJECTED"
	NotificationSeen         NotificationReceipt = "SEEN"
	NotificationSent         NotificationReceipt = "SENT"
	NotificationFailedToSend NotificationReceipt = "FAILED_TO_SEND"

	NotificationDirectionSent     NotificationDirection = "sent"
	NotificationDirectionReceived NotificationDirection = "received"

	NotificationKindNote      NotificationKind = "note"
	NotificationKindCondition NotificationKind = "condition"
)

type PrItem struct {
	ID         string    `json:"id"`
	Barcode    string    `json:"barcode"`
	CallNumber *string   `json:"call_number"`
	Title      *string   `json:"title"`
	ItemID     *string   `json:"item_id"`
	CreatedAt  time.Time `json:"created_at"`
}
