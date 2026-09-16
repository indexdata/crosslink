package lms

import "github.com/indexdata/crosslink/broker/ncipclient"

type LmsAdapterManual struct {
}

func (l *LmsAdapterManual) SetLogFunc(logFunc ncipclient.NcipLogFunc) {
}

func (l *LmsAdapterManual) LookupUser(patron string, validatePatronProfile bool) (string, error) {
	return patron, nil
}

// AcceptItem skips requester LMS item creation in manual workflows.
func (l *LmsAdapterManual) AcceptItem(
	itemId string,
	requestId string,
	userId string,
	author string,
	title string,
	isbn string,
	callNumber string,
	pickupLocation string,
	requestedAction string,
) (bool, error) {
	return false, nil
}

// DeleteItem skips requester LMS item deletion in manual workflows.
func (l *LmsAdapterManual) DeleteItem(itemId string) (bool, error) {
	return false, nil
}

func (l *LmsAdapterManual) RequestItem(
	requestId string,
	itemId string,
	userId string,
	pickupLocation string,
	itemLocation string,
) (*RequestedItem, error) {
	return nil, nil
}

func (l *LmsAdapterManual) CancelRequestItem(requestId string, userId string) error {
	return nil
}

// CheckInItem records an explicit manual check-in confirmation.
func (l *LmsAdapterManual) CheckInItem(itemId string) (bool, error) {
	return true, nil
}

// CheckOutItem records an explicit manual checkout confirmation without an LMS date.
func (l *LmsAdapterManual) CheckOutItem(
	requestId string,
	itemBarcode string,
	userId string,
	externalReferenceValue string,
) (*CheckedOutItem, error) {
	return &CheckedOutItem{}, nil
}

func (l *LmsAdapterManual) CreateUserFiscalTransaction(userId string, itemId string) error {
	return nil
}

func CreateLmsAdapterMockOK() LmsAdapter {
	return &LmsAdapterManual{}
}

func (l *LmsAdapterManual) InstitutionalPatron(requesterSymbol string) string {
	return ""
}

func (l *LmsAdapterManual) SupplierPickupLocation() string {
	return ""
}

func (l *LmsAdapterManual) ItemLocation() string {
	return ""
}

func (l *LmsAdapterManual) RequesterPickupLocation() string {
	return ""
}

func (l *LmsAdapterManual) RequestItemUsesPickupLocation() bool { return false }
func (l *LmsAdapterManual) AcceptItemUsesPickupLocation() bool  { return false }
