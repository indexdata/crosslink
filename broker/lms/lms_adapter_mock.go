package lms

type LmsAdapterMock struct {
}

func (l *LmsAdapterMock) LookupUser(patron string) (string, error) {
	return patron, nil
}

func (l *LmsAdapterMock) AcceptItem(
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

func (l *LmsAdapterMock) DeleteItem(itemId string) error {
	return nil
}

func (l *LmsAdapterMock) RequestItem(
	requestId string,
	itemId string,
	borrowerBarcode string,
	pickupLocation string,
	itemLocation string,
) error {
	return nil
}

func (l *LmsAdapterMock) CancelRequestItem(requestId string, userId string) error {
	return nil
}

func (l *LmsAdapterMock) CheckInItem(itemId string) error {
	return nil
}

func (l *LmsAdapterMock) CheckOutItem(
	requestId string,
	itemBarcode string,
	borrowerBarcode string,
	externalReferenceValue string,
) error {
	return nil
}

func (l *LmsAdapterMock) CreateUserFiscalTransaction(userId string, itemId string) error {
	return nil
}

func CreateLmsAdapterMockOK() LmsAdapter {
	return &LmsAdapterMock{}
}
