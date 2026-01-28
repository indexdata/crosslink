package lms

type LmsAdapterManual struct {
}

func (l *LmsAdapterManual) LookupUser(patron string) (string, error) {
	return patron, nil
}

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
) error {
	return nil
}

func (l *LmsAdapterManual) DeleteItem(itemId string) error {
	return nil
}

func (l *LmsAdapterManual) RequestItem(
	requestId string,
	itemId string,
	userId string,
	pickupLocation string,
	itemLocation string,
) error {
	return nil
}

func (l *LmsAdapterManual) CancelRequestItem(requestId string, userId string) error {
	return nil
}

func (l *LmsAdapterManual) CheckInItem(itemId string) error {
	return nil
}

func (l *LmsAdapterManual) CheckOutItem(
	requestId string,
	itemId string,
	userId string,
	externalReferenceValue string,
) error {
	return nil
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
