package lms

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/assert"
)

func TestCreateLmsAdapterNcip(t *testing.T) {
	config := directory.LmsConfig{
		Address:    "http://ncip.example.com",
		FromAgency: "MyAgency",
	}
	ad, err := CreateLmsAdapterNcip(config)
	assert.NoError(t, err)
	assert.NotNil(t, ad)

	config = directory.LmsConfig{
		FromAgency: "MyAgency",
	}
	_, err = CreateLmsAdapterNcip(config)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in LMS configuration", err.Error())

	config = directory.LmsConfig{
		Address: "http://ncip.example.com",
	}
	_, err = CreateLmsAdapterNcip(config)
	assert.Error(t, err)
	assert.Equal(t, "missing From Agency in LMS configuration", err.Error())
}

func TestLookupUser(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	config := directory.LmsConfig{
		LookupUserEnabled: &b,
	}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	_, err := ad.LookupUser("")
	assert.Error(t, err)
	assert.Equal(t, "empty patron identifier", err.Error())

	userId, err := ad.LookupUser("testuser")
	assert.NoError(t, err)
	assert.Equal(t, "testuser", userId)

	_, err = ad.LookupUser("bad user")
	assert.Error(t, err)
	assert.Equal(t, "unknown user name", err.Error())

	userId, err = ad.LookupUser("pass")
	assert.NoError(t, err)
	assert.Equal(t, "pass", userId)

	_, err = ad.LookupUser("missing data")
	assert.Error(t, err)
	assert.Equal(t, "missing User ID in LookupUser response", err.Error())

	userId, err = ad.LookupUser("good user")
	assert.NoError(t, err)
	assert.Equal(t, "user124", userId)

	userId, err = ad.LookupUser("other user")
	assert.NoError(t, err)
	assert.Equal(t, "user123", userId)

	b = false
	userId, err = ad.LookupUser("")
	assert.NoError(t, err)
	assert.Equal(t, "", userId)

	mock.(*ncipClientMock).lastRequest = nil
	userId, err = ad.LookupUser("anyuser")
	assert.NoError(t, err)
	assert.Equal(t, "anyuser", userId)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest) // not called
}

func TestAcceptItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	ad := &LmsAdapterNcip{
		config:     directory.LmsConfig{AcceptItemEnabled: &b},
		ncipClient: mock,
	}
	err := ad.AcceptItem("item1", "req1", "testuser", "author", "title", "isbn", "callnum", "loc", "action")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.AcceptItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "author", req.ItemOptionalFields.BibliographicDescription.Author)
	assert.Equal(t, "title", req.ItemOptionalFields.BibliographicDescription.Title)
	assert.Equal(t, "isbn", req.ItemOptionalFields.BibliographicDescription.BibliographicItemId.BibliographicItemIdentifier)
	assert.Equal(t, "ISBN", req.ItemOptionalFields.BibliographicDescription.BibliographicItemId.BibliographicItemIdentifierCode.Text)
	assert.Equal(t, "callnum", req.ItemOptionalFields.ItemDescription.CallNumber)
	assert.Equal(t, "loc", req.PickupLocation.Text)
	assert.Equal(t, "action", req.RequestedActionType.Text)

	err = ad.AcceptItem("item1", "req1", "testuser", "author", "title", "", "", "", "")
	assert.NoError(t, err)
	req = mock.(*ncipClientMock).lastRequest.(ncip.AcceptItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "author", req.ItemOptionalFields.BibliographicDescription.Author)
	assert.Equal(t, "title", req.ItemOptionalFields.BibliographicDescription.Title)
	assert.Nil(t, req.ItemOptionalFields.BibliographicDescription.BibliographicItemId)
	assert.Nil(t, req.ItemOptionalFields.ItemDescription)
	assert.Nil(t, req.PickupLocation)
	assert.Equal(t, "Hold For Pickup", req.RequestedActionType.Text)

	b = false
	mock.(*ncipClientMock).lastRequest = nil
	err = ad.AcceptItem("", "", "", "", "", "", "", "", "")
	assert.NoError(t, err)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest)
}

func TestDeleteItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		ncipClient: mock,
	}
	err := ad.DeleteItem("item1")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.DeleteItem)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)

	err = ad.DeleteItem("error")
	assert.Error(t, err)
	assert.Equal(t, "deletion error", err.Error())
}

func TestRequestItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	loan := "Loan"
	title := "Title"
	sysnumber := "SYSNUMBER"
	itemLocation := "itemloc"
	ad := &LmsAdapterNcip{
		config: directory.LmsConfig{
			ItemLocation:                     &itemLocation,
			RequestItemPickupLocationEnabled: &b,
			RequestItemRequestType:           &loan,
			RequestItemRequestScopeType:      &title,
			RequestItemBibIdCode:             &sysnumber,
		},
		ncipClient: mock,
	}
	err := ad.RequestItem("req1", "item1", "testuser", "pickloc", itemLocation)
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "item1", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	assert.Equal(t, "SYSNUMBER", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "pickloc", req.PickupLocation.Text)
	assert.Equal(t, "Loan", req.RequestType.Text)
	assert.Equal(t, "Title", req.RequestScopeType.Text)
	assert.Equal(t, itemLocation, req.ItemOptionalFields.Location[0].LocationName.LocationNameInstance[0].LocationNameValue)

	b = false
	ad = &LmsAdapterNcip{
		config: directory.LmsConfig{
			RequestItemPickupLocationEnabled: &b,
		},
		ncipClient: mock,
	}
	mock.(*ncipClientMock).lastRequest = nil
	err = ad.RequestItem("req1", "item1", "testuser", "pickloc", "")
	assert.NoError(t, err)
	req = mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Nil(t, req.PickupLocation)
	assert.Nil(t, req.ItemOptionalFields)
}

func TestCancelRequestItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		ncipClient: mock,
	}
	err := ad.CancelRequestItem("req1", "testuser")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CancelRequestItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
}

func TestCheckInItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config: directory.LmsConfig{
			CheckInItemEnabled: &b,
		},
	}
	err := ad.CheckInItem("item1")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CheckInItem)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, 1, len(req.ItemElementType))
	assert.Equal(t, "Bibliographic Description", req.ItemElementType[0].Text)

	b = false
	mock.(*ncipClientMock).lastRequest = nil
	err = ad.CheckInItem("item1")
	assert.NoError(t, err)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest)
}

func TestCheckOutItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config: directory.LmsConfig{
			CheckOutItemEnabled: &b,
		},
	}
	ref := "extref"
	err := ad.CheckOutItem("req1", "item1", "barcodeid", ref)
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CheckOutItem)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, "barcodeid", req.UserId.UserIdentifierValue)
	bytes, err := xml.Marshal(ncip.RequestId{RequestIdentifierValue: ref})
	assert.NoError(t, err)
	assert.Equal(t, bytes, req.Ext.XMLContent)

	ref = "\x10" // will be replaced with replacement character
	err = ad.CheckOutItem("req1", "item1", "barcodeid", ref)
	assert.NoError(t, err)
	req = mock.(*ncipClientMock).lastRequest.(ncip.CheckOutItem)
	bytes, err = xml.Marshal(ncip.RequestId{RequestIdentifierValue: ref})
	assert.NoError(t, err)
	assert.Equal(t, bytes, req.Ext.XMLContent)

	b = false
	mock.(*ncipClientMock).lastRequest = nil
	err = ad.CheckOutItem("req1", "item1", "barcodeid", "extref")
	assert.NoError(t, err)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest)
}

func TestCreateUserFiscalTransaction(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		ncipClient: mock,
	}
	err := ad.CreateUserFiscalTransaction("testuser", "item1")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CreateUserFiscalTransaction)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
}

func TestInstitutionalPatron(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	config := directory.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	institutionalPatron := ad.InstitutionalPatron("123456")
	assert.Equal(t, "INST-123456", institutionalPatron)

	p := "USER-{requesterSymbol}-XYZ"
	config = directory.LmsConfig{RequesterPatronPattern: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	institutionalPatron = ad.InstitutionalPatron("123456")
	assert.Equal(t, "USER-123456-XYZ", institutionalPatron)
}

func TestSupplierPickupLocation(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	config := directory.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation := ad.SupplierPickupLocation()
	assert.Equal(t, "ILL Office", pickupLocation)

	p := "Office2"
	config = directory.LmsConfig{SupplierPickupLocation: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation = ad.SupplierPickupLocation()
	assert.Equal(t, "Office2", pickupLocation)
}

func TestRequesterPickupLocation(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	config := directory.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation := ad.RequesterPickupLocation()
	assert.Equal(t, "Main Library", pickupLocation)

	p := "3rd Floor Desk"
	config = directory.LmsConfig{RequesterPickupLocation: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation = ad.RequesterPickupLocation()
	assert.Equal(t, "3rd Floor Desk", pickupLocation)
}

type ncipClientMock struct {
	lastRequest any
}

func (n *ncipClientMock) LookupUser(lookup ncip.LookupUser) (*ncip.LookupUserResponse, error) {
	n.lastRequest = lookup
	if lookup.UserId != nil {
		if lookup.UserId.UserIdentifierValue == "pass" {
			return nil, nil
		}
		if strings.Contains(lookup.UserId.UserIdentifierValue, " ") {
			return nil, fmt.Errorf("unknown user id")
		}
		return &ncip.LookupUserResponse{
			UserId: &ncip.UserId{UserIdentifierValue: lookup.UserId.UserIdentifierValue},
		}, nil
	}
	if lookup.AuthenticationInput[0].AuthenticationInputData == "bad user" {
		return nil, fmt.Errorf("unknown user name")
	}
	if lookup.AuthenticationInput[0].AuthenticationInputData == "missing data" {
		return &ncip.LookupUserResponse{}, nil
	}
	if lookup.AuthenticationInput[0].AuthenticationInputData == "good user" {
		return &ncip.LookupUserResponse{
			UserOptionalFields: &ncip.UserOptionalFields{
				UserId: []ncip.UserId{
					{UserIdentifierValue: "user124"},
				},
			},
		}, nil
	}
	return &ncip.LookupUserResponse{
		UserId: &ncip.UserId{UserIdentifierValue: "user123"},
	}, nil
}

func (n *ncipClientMock) AcceptItem(accept ncip.AcceptItem) (*ncip.AcceptItemResponse, error) {
	n.lastRequest = accept
	return nil, nil
}

func (n *ncipClientMock) DeleteItem(delete ncip.DeleteItem) (*ncip.DeleteItemResponse, error) {
	if delete.ItemId.ItemIdentifierValue == "error" {
		return nil, fmt.Errorf("deletion error")
	}
	n.lastRequest = delete
	return nil, nil
}

func (n *ncipClientMock) RequestItem(request ncip.RequestItem) (*ncip.RequestItemResponse, error) {
	n.lastRequest = request
	return nil, nil
}

func (n *ncipClientMock) CancelRequestItem(cancel ncip.CancelRequestItem) (*ncip.CancelRequestItemResponse, error) {
	n.lastRequest = cancel
	return nil, nil
}

func (n *ncipClientMock) CheckInItem(checkin ncip.CheckInItem) (*ncip.CheckInItemResponse, error) {
	n.lastRequest = checkin
	return nil, nil
}

func (n *ncipClientMock) CheckOutItem(checkout ncip.CheckOutItem) (*ncip.CheckOutItemResponse, error) {
	n.lastRequest = checkout
	return nil, nil
}

func (n *ncipClientMock) CreateUserFiscalTransaction(create ncip.CreateUserFiscalTransaction) (*ncip.CreateUserFiscalTransactionResponse, error) {
	n.lastRequest = create
	return nil, nil
}
