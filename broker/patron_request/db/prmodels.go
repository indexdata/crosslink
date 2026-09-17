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

// LmsStatus is an item's last confirmed LMS operation, independent of request state.
type LmsStatus string

const (
	// LmsStatusUnknown means no LMS operation has been confirmed.
	LmsStatusUnknown LmsStatus = "UNKNOWN"
	// LmsStatusRequested records a successful supplier RequestItem.
	LmsStatusRequested LmsStatus = "REQUESTED"
	// LmsStatusAccepted records a successful requester AcceptItem.
	LmsStatusAccepted LmsStatus = "ACCEPTED"
	// LmsStatusCheckedOut records checkout or explicit manual confirmation.
	LmsStatusCheckedOut LmsStatus = "CHECKED_OUT"
	// LmsStatusCheckedIn records check-in or explicit manual confirmation.
	LmsStatusCheckedIn LmsStatus = "CHECKED_IN"
	// LmsStatusDeleted records confirmed deletion of the requester LMS item.
	LmsStatusDeleted LmsStatus = "DELETED"
)

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
	LmsStatus  LmsStatus  `json:"lms_status"`
	LmsDueDate *time.Time `json:"lms_due_date"`
	ID         string     `json:"id"`
	Barcode    string     `json:"barcode"`
	CallNumber *string    `json:"call_number"`
	Title      *string    `json:"title"`
	ItemID     *string    `json:"item_id"`
	CreatedAt  time.Time  `json:"created_at"`
}
