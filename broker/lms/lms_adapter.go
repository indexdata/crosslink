package lms

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/ncipclient"
)

// PatronProfileIneligibleError indicates that a patron was found in the LMS,
// but their configured profile does not permit creating ILL requests.
type PatronProfileIneligibleError struct {
	ProfileCode string
	ProfileName string
}

func (e *PatronProfileIneligibleError) Error() string {
	return fmt.Sprintf("patron profile with code %q and name %q is not eligible to create ILL requests", e.ProfileCode, e.ProfileName)
}

// RequestedItem contains data returned by a performed LMS RequestItem
// call. A nil response with a nil error means the adapter intentionally skipped
// the operation, for example because RequestItem is disabled or handled manually.
type RequestedItem struct {
	RequestID  string
	Barcode    string
	CallNumber string
	Title      string
}

// LmsAdapter is an interface defining methods for interacting with a Library Management System (LMS)
// https://github.com/openlibraryenvironment/mod-rs/blob/master/service/src/main/groovy/org/olf/rs/lms/HostLMSActions.groovy
type LmsAdapter interface {
	SetLogFunc(logFunc ncipclient.NcipLogFunc)

	LookupUser(patron string, validatePatronProfile bool) (userId string, err error)

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
	) (*RequestedItem, error)

	CancelRequestItem(requestId string, userId string) error

	CheckInItem(itemId string) error

	CheckOutItem(
		requestId string,
		itemBarcode string,
		userId string,
		externalReferenceValue string,
	) (title string, err error)

	CreateUserFiscalTransaction(userId string, itemId string) error

	InstitutionalPatron(requesterSymbol string) string

	SupplierPickupLocation() string

	ItemLocation() string

	RequesterPickupLocation() string
}
