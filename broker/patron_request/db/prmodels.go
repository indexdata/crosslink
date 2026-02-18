package pr_db

type PatronRequestState string
type PatronRequestSide string
type PatronRequestAction string
type NotificationReceipt string

const (
	NotificationAccepted NotificationReceipt = "ACCEPTED"
	NotificationRejected NotificationReceipt = "REJECTED"
	NotificationSeen     NotificationReceipt = "SEEN"
)
