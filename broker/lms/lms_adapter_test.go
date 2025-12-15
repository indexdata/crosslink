package lms

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/ncipclient"
	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/assert"
)

func TestParseConfigFull(t *testing.T) {
	ncipConfig := map[string]any{
		"address":                    "http://ncip.example.com",
		"from_agency":                "AGENCY1",
		"to_agency":                  "AGENCY2",
		"from_agency_authentication": "auth",
		"lookup_user_enable":         false,
		"accept_item_enable":         false,
		"check_in_item_enable":       false,
		"check_out_item_enable":      false,
	}
	lmsAdapaterNcip := &LmsAdapterNcip{}
	err := lmsAdapaterNcip.parseConfig(ncipConfig)
	assert.NoError(t, err)
	assert.Equal(t, "http://ncip.example.com", lmsAdapaterNcip.address)
	assert.Equal(t, "AGENCY1", lmsAdapaterNcip.fromAgency)
	assert.Equal(t, "AGENCY2", lmsAdapaterNcip.toAgency)
	assert.Equal(t, "auth", lmsAdapaterNcip.fromAgencyAuthentication)
	assert.False(t, lmsAdapaterNcip.lookupUserEnable)
	assert.False(t, lmsAdapaterNcip.acceptItemEnable)
	assert.False(t, lmsAdapaterNcip.checkInItemEnable)
}

func TestParseConfigOptional(t *testing.T) {
	// Missing optional to_agency and from_agency_authentication fields
	ncipConfig := map[string]any{
		"address":                    "http://ncip.example.com",
		"from_agency":                "AGENCY1",
		"from_agency_authentication": "auth",
	}
	lmsAdapaterNcip := &LmsAdapterNcip{}
	err := lmsAdapaterNcip.parseConfig(ncipConfig)
	assert.NoError(t, err)
	assert.Equal(t, "http://ncip.example.com", lmsAdapaterNcip.address)
	assert.Equal(t, "AGENCY1", lmsAdapaterNcip.fromAgency)
	assert.Equal(t, "default-to-agency", lmsAdapaterNcip.toAgency)
	assert.Equal(t, "auth", lmsAdapaterNcip.fromAgencyAuthentication)
	assert.True(t, lmsAdapaterNcip.lookupUserEnable)
	assert.True(t, lmsAdapaterNcip.acceptItemEnable)
	assert.True(t, lmsAdapaterNcip.checkInItemEnable)
}

func TestParseConfigMissing(t *testing.T) {
	// Missing required address field
	ncipConfigMissing := map[string]any{
		"from_agency": "AGENCY1",
	}
	lmsAdapaterNcip := &LmsAdapterNcip{}
	err := lmsAdapaterNcip.parseConfig(ncipConfigMissing)
	assert.Error(t, err)
	assert.Equal(t, "missing required NCIP configuration field: address", err.Error())

	// Missing required from_agency field
	ncipConfigMissing = map[string]any{
		"address": "http://ncip.example.com",
	}
	lmsAdapaterNcip = &LmsAdapterNcip{}
	err = lmsAdapaterNcip.parseConfig(ncipConfigMissing)
	assert.Error(t, err)
	assert.Equal(t, "missing required NCIP configuration field: from_agency", err.Error())
}

func TestLookupUser(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		ncipClient:       mock,
		lookupUserEnable: true,
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

	ad.lookupUserEnable = false
	userId, err = ad.LookupUser("")
	assert.NoError(t, err)
	assert.Equal(t, "", userId)

	ad.lookupUserEnable = false
	mock.(*ncipClientMock).lastRequest = nil
	userId, err = ad.LookupUser("anyuser")
	assert.NoError(t, err)
	assert.Equal(t, "anyuser", userId)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest) // not called
}

func TestAcceptItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		acceptItemEnable: true,
		ncipClient:       mock,
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

	ad.acceptItemEnable = false
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
	ad := &LmsAdapterNcip{
		ncipClient: mock,
	}
	err := ad.RequestItem("req1", "item1", "testuser", "loc", "itemloc")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "item1", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "loc", req.PickupLocation.Text)
	assert.Equal(t, "Page", req.RequestType.Text)
	assert.Equal(t, "Item", req.RequestScopeType.Text)
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
	ad := &LmsAdapterNcip{
		ncipClient:        mock,
		checkInItemEnable: true,
	}
	err := ad.CheckInItem("item1")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CheckInItem)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, 1, len(req.ItemElementType))
	assert.Equal(t, "Bibliographic Description", req.ItemElementType[0].Text)

	ad.checkInItemEnable = false
	mock.(*ncipClientMock).lastRequest = nil
	err = ad.CheckInItem("item1")
	assert.NoError(t, err)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest)
}

func TestCheckOutItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		ncipClient:         mock,
		checkOutItemEnable: true,
	}
	err := ad.CheckOutItem("req1", "item1", "barcodeid", "extref")
	assert.NoError(t, err)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CheckOutItem)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, "barcodeid", req.UserId.UserIdentifierValue)
	bytes, err := xml.Marshal(ncip.RequestId{RequestIdentifierValue: "extref"})
	assert.NoError(t, err)
	assert.Equal(t, bytes, req.Ext.XMLContent)

	ad.checkOutItemEnable = false
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
