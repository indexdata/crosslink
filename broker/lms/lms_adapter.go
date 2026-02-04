package lms

// LmsAdapter is an interface defining methods for interacting with a Library Management System (LMS)
// https://github.com/openlibraryenvironment/mod-rs/blob/master/service/src/main/groovy/org/olf/rs/lms/HostLMSActions.groovy
type LmsAdapter interface {
	LookupUser(patron string) (string, error)

	AcceptItem(
		itemId string,
		requestId string,
		userId string,
		author string,
		title string,
		isbn string,
		callNumber string,
		pickupLocation string,
		requestedAction string,
	) error

	DeleteItem(itemId string) error

	RequestItem(
		requestId string,
		itemId string,
		userId string,
		pickupLocation string,
		itemLocation string,
	) error

	CancelRequestItem(requestId string, userId string) error

	CheckInItem(itemId string) error

	CheckOutItem(
		requestId string,
		itemId string,
		userId string,
		externalReferenceValue string,
	) error

	CreateUserFiscalTransaction(userId string, itemId string) error

	InstitutionalPatron(requesterSymbol string) string

	SupplierPickupLocation() string

	ItemLocation() string

	RequesterPickupLocation() string
}
