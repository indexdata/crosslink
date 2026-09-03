package lms

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/ncipclient"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/ncip"
	"github.com/stretchr/testify/assert"
)

func TestCreateLmsAdapterNcip(t *testing.T) {
	config := dirapi.LmsConfig{
		Address:    "http://ncip.example.com",
		FromAgency: "MyAgency",
	}
	ad, err := CreateLmsAdapterNcip(config)
	assert.NoError(t, err)
	assert.NotNil(t, ad)

	config = dirapi.LmsConfig{
		FromAgency: "MyAgency",
	}
	_, err = CreateLmsAdapterNcip(config)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in LMS configuration", err.Error())

	config = dirapi.LmsConfig{
		Address: "http://ncip.example.com",
	}
	_, err = CreateLmsAdapterNcip(config)
	assert.Error(t, err)
	assert.Equal(t, "missing From Agency in LMS configuration", err.Error())
}

func TestLookupUser(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	profiles := dirapi.PatronProfiles{
		{Code: strPtr("STAFF"), Name: strPtr("Staff"), CanCreateRequests: true},
		{Code: strPtr("BLOCKED"), Name: strPtr("Blocked patrons"), CanCreateRequests: false},
	}
	config := dirapi.LmsConfig{
		LookupUserEnabled: &b,
		PatronProfiles:    &profiles,
	}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	_, err := ad.LookupUser("", true)
	assert.Error(t, err)
	assert.Equal(t, "empty patron identifier", err.Error())

	userId, err := ad.LookupUser("testuser", true)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", userId)
	request := mock.(*ncipClientMock).lastRequest.(ncip.LookupUser)
	assert.Equal(t, []ncip.SchemeValuePair{{Text: NCIPUserId}, {Text: NCIPUserPrivilege}}, request.UserElementType)

	userId, err = ad.LookupUser("staff-profile", true)
	assert.NoError(t, err)
	assert.Equal(t, "staff-profile", userId)

	_, err = ad.LookupUser("blocked-profile", true)
	assert.EqualError(t, err, `patron profile with code "BLOCKED" and name "Blocked patrons" is not eligible to create ILL requests`)
	var ineligibleErr *PatronProfileIneligibleError
	if assert.ErrorAs(t, err, &ineligibleErr) {
		assert.Equal(t, "BLOCKED", ineligibleErr.ProfileCode)
		assert.Equal(t, "Blocked patrons", ineligibleErr.ProfileName)
	}

	_, err = ad.LookupUser("blocked user", true)
	assert.EqualError(t, err, `patron profile with code "BLOCKED" and name "Blocked patrons" is not eligible to create ILL requests`)
	request = mock.(*ncipClientMock).lastRequest.(ncip.LookupUser)
	assert.Equal(t, []ncip.SchemeValuePair{{Text: NCIPUserId}, {Text: NCIPUserPrivilege}}, request.UserElementType)

	userId, err = ad.LookupUser("blocked-profile", false)
	assert.NoError(t, err)
	assert.Equal(t, "blocked-profile", userId)
	request = mock.(*ncipClientMock).lastRequest.(ncip.LookupUser)
	assert.Empty(t, request.UserElementType)

	userId, err = ad.LookupUser("blocked user", false)
	assert.NoError(t, err)
	assert.Equal(t, "blocked-user-id", userId)
	request = mock.(*ncipClientMock).lastRequest.(ncip.LookupUser)
	assert.Equal(t, []ncip.SchemeValuePair{{Text: NCIPUserId}}, request.UserElementType)

	_, err = ad.LookupUser("bad user", true)
	assert.Error(t, err)
	assert.Equal(t, "unknown user name", err.Error())

	_, err = ad.LookupUser("problem user", true)
	var ncipErr *ncipclient.NcipError
	assert.ErrorAs(t, err, &ncipErr)
	assert.Equal(t, string(ncip.UnknownUser), ncipErr.Problem.ProblemType.Text)
	assert.Equal(t, "patron was not found", ncipErr.Problem.ProblemDetail)

	userId, err = ad.LookupUser("pass", true)
	assert.NoError(t, err)
	assert.Equal(t, "pass", userId)

	_, err = ad.LookupUser("missing data", true)
	assert.Error(t, err)
	assert.Equal(t, "missing User ID in LookupUser response", err.Error())

	userId, err = ad.LookupUser("good user", true)
	assert.NoError(t, err)
	assert.Equal(t, "user124", userId)

	userId, err = ad.LookupUser("other user", true)
	assert.NoError(t, err)
	assert.Equal(t, "user123", userId)

	b = false
	userId, err = ad.LookupUser("", true)
	assert.NoError(t, err)
	assert.Equal(t, "", userId)

	mock.(*ncipClientMock).lastRequest = nil
	userId, err = ad.LookupUser("anyuser", true)
	assert.NoError(t, err)
	assert.Equal(t, "anyuser", userId)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest) // not called
}

func TestLookupUserElements(t *testing.T) {
	emptyProfiles := dirapi.PatronProfiles{}
	configuredProfiles := dirapi.PatronProfiles{{CanCreateRequests: true}}
	userIDElement := []ncip.SchemeValuePair{{Text: NCIPUserId}}
	profileElements := []ncip.SchemeValuePair{{Text: NCIPUserId}, {Text: NCIPUserPrivilege}}

	tests := []struct {
		name             string
		profiles         *dirapi.PatronProfiles
		directElements   []ncip.SchemeValuePair
		fallbackElements []ncip.SchemeValuePair
	}{
		{
			name:             "profiles omitted",
			fallbackElements: userIDElement,
		},
		{
			name:             "profiles empty",
			profiles:         &emptyProfiles,
			fallbackElements: userIDElement,
		},
		{
			name:             "profiles configured",
			profiles:         &configuredProfiles,
			directElements:   profileElements,
			fallbackElements: profileElements,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mock := new(ncipClientMock)
			adapter := &LmsAdapterNcip{
				ncipClient: mock,
				config:     dirapi.LmsConfig{PatronProfiles: test.profiles},
			}

			_, err := adapter.LookupUser("testuser", true)
			assert.NoError(t, err)
			directRequest := mock.lastRequest.(ncip.LookupUser)
			assert.Equal(t, test.directElements, directRequest.UserElementType)

			_, err = adapter.LookupUser("other user", true)
			assert.NoError(t, err)
			fallbackRequest := mock.lastRequest.(ncip.LookupUser)
			assert.Equal(t, test.fallbackElements, fallbackRequest.UserElementType)

			_, err = adapter.LookupUser("testuser", false)
			assert.NoError(t, err)
			directRequest = mock.lastRequest.(ncip.LookupUser)
			assert.Empty(t, directRequest.UserElementType)

			_, err = adapter.LookupUser("other user", false)
			assert.NoError(t, err)
			fallbackRequest = mock.lastRequest.(ncip.LookupUser)
			assert.Equal(t, userIDElement, fallbackRequest.UserElementType)
		})
	}
}

func TestPatronProfile(t *testing.T) {
	tests := []struct {
		name             string
		privileges       []ncip.UserPrivilege
		expectCandidates []patronProfileCandidate
	}{
		{
			name:             "profile status value",
			privileges:       []ncip.UserPrivilege{testUserPrivilege("PROFILE", "STAFF", "Staff")},
			expectCandidates: []patronProfileCandidate{{code: "STAFF", name: "Staff"}},
		},
		{
			name:             "active profile privilege",
			privileges:       []ncip.UserPrivilege{testUserPrivilege("STAFF", "Active", "Staff")},
			expectCandidates: []patronProfileCandidate{{code: "STAFF", name: "Staff"}},
		},
		{
			name: "OK profile among other privileges",
			privileges: []ncip.UserPrivilege{
				testUserPrivilege("STAFF", "01/01/01", "Staff"),
				testUserPrivilege("STAFF", "OK", "Staff"),
			},
			expectCandidates: []patronProfileCandidate{{code: "STAFF", name: "Staff"}},
		},
		{
			name: "multiple active privileges",
			privileges: []ncip.UserPrivilege{
				testUserPrivilege("BORROWING", "Active", "Borrowing enabled"),
				testUserPrivilege("STAFF", "Active", "Staff"),
			},
			expectCandidates: []patronProfileCandidate{
				{code: "BORROWING", name: "Borrowing enabled"},
				{code: "STAFF", name: "Staff"},
			},
		},
		{
			name:             "single privilege without status",
			privileges:       []ncip.UserPrivilege{testUserPrivilege("STAFF", "", "Staff")},
			expectCandidates: []patronProfileCandidate{{code: "STAFF", name: "Staff"}},
		},
		{
			name:       "profile unavailable",
			privileges: []ncip.UserPrivilege{testUserPrivilege("BORROWING", "DENIED", "")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &ncip.LookupUserResponse{
				UserOptionalFields: &ncip.UserOptionalFields{UserPrivilege: test.privileges},
			}
			assert.Equal(t, test.expectCandidates, patronProfileCandidates(response))
		})
	}
	assert.Empty(t, patronProfileCandidates(nil))
}

func TestValidatePatronProfileRules(t *testing.T) {
	response := lookupUserResponseWithProfile("user-id", "PROFILE", "STAFF", "Staff")

	nameOnly := dirapi.PatronProfiles{{Name: strPtr("Staff"), CanCreateRequests: false}}
	adapter := LmsAdapterNcip{config: dirapi.LmsConfig{PatronProfiles: &nameOnly}}
	assert.Error(t, adapter.validatePatronProfile(response))

	codeOnly := dirapi.PatronProfiles{{Code: strPtr("staff"), CanCreateRequests: true}}
	adapter.config.PatronProfiles = &codeOnly
	assert.NoError(t, adapter.validatePatronProfile(response))

	firstMatchWins := dirapi.PatronProfiles{
		{Code: strPtr("STAFF"), Name: strPtr("Staff"), CanCreateRequests: true},
		{CanCreateRequests: false},
	}
	adapter.config.PatronProfiles = &firstMatchWins
	assert.NoError(t, adapter.validatePatronProfile(response))

	bothComponentsMustMatch := dirapi.PatronProfiles{
		{Code: strPtr("STAFF"), Name: strPtr("Student"), CanCreateRequests: false},
		{CanCreateRequests: true},
	}
	adapter.config.PatronProfiles = &bothComponentsMustMatch
	assert.NoError(t, adapter.validatePatronProfile(response))

	defaultDenied := dirapi.PatronProfiles{
		{Code: strPtr("OTHER"), CanCreateRequests: true},
		{CanCreateRequests: false},
	}
	adapter.config.PatronProfiles = &defaultDenied
	assert.Error(t, adapter.validatePatronProfile(response))
	assert.Error(t, adapter.validatePatronProfile(nil))

	noMatch := dirapi.PatronProfiles{{Code: strPtr("OTHER"), CanCreateRequests: false}}
	adapter.config.PatronProfiles = &noMatch
	assert.NoError(t, adapter.validatePatronProfile(response))

	multipleActivePrivileges := &ncip.LookupUserResponse{
		UserOptionalFields: &ncip.UserOptionalFields{UserPrivilege: []ncip.UserPrivilege{
			testUserPrivilege("BORROWING", "Active", "Borrowing enabled"),
			testUserPrivilege("STAFF", "Active", "Staff"),
		}},
	}
	denyStaff := dirapi.PatronProfiles{
		{Code: strPtr("STAFF"), CanCreateRequests: false},
		{CanCreateRequests: true},
	}
	adapter.config.PatronProfiles = &denyStaff
	err := adapter.validatePatronProfile(multipleActivePrivileges)
	var ineligibleErr *PatronProfileIneligibleError
	if assert.ErrorAs(t, err, &ineligibleErr) {
		assert.Equal(t, "STAFF", ineligibleErr.ProfileCode)
		assert.Equal(t, "Staff", ineligibleErr.ProfileName)
	}
}

func testUserPrivilege(privilegeType string, status string, description string) ncip.UserPrivilege {
	privilege := ncip.UserPrivilege{
		AgencyUserPrivilegeType:  ncip.SchemeValuePair{Text: privilegeType},
		UserPrivilegeDescription: description,
	}
	if status != "" {
		privilege.UserPrivilegeStatus = &ncip.UserPrivilegeStatus{
			UserPrivilegeStatusType: ncip.SchemeValuePair{Text: status},
		}
	}
	return privilege
}

func TestAcceptItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	ad := &LmsAdapterNcip{
		config:     dirapi.LmsConfig{AcceptItemEnabled: &b},
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
	sysnumber := "NUMBER"
	itemLocation := "itemloc"
	ad := &LmsAdapterNcip{
		config: dirapi.LmsConfig{
			ItemLocation:                     &itemLocation,
			RequestItemPickupLocationEnabled: &b,
			RequestItemRequestType:           &loan,
			RequestItemRequestScopeType:      &title,
			RequestItemBibIdCode:             &sysnumber,
		},
		ncipClient: mock,
	}
	response, err := ad.RequestItem("req1", "item1", "testuser", "pickloc", itemLocation)
	assert.NoError(t, err)
	assert.Equal(t, "123.456", response.Barcode)
	assert.Equal(t, "QA123 .A45", response.CallNumber)
	assert.Equal(t, "", response.Title)
	assert.Equal(t, "lms-req-1", response.RequestID)
	req := mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "item1", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	assert.Equal(t, "NUMBER", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "pickloc", req.PickupLocation.Text)
	assert.Equal(t, "Loan", req.RequestType.Text)
	assert.Equal(t, "Title", req.RequestScopeType.Text)
	assert.Equal(t, itemLocation, req.ItemOptionalFields.Location[0].LocationName.LocationNameInstance[0].LocationNameValue)

	ad = &LmsAdapterNcip{
		config:     dirapi.LmsConfig{},
		ncipClient: mock,
	}
	mock.(*ncipClientMock).honorTitle = true
	response, err = ad.RequestItem("req1", "item1", "testuser", "loc", "itemloc")
	assert.NoError(t, err)
	req = mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Equal(t, "123.456", response.Barcode)
	assert.Equal(t, "QA123 .A45", response.CallNumber)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "item1", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	assert.Equal(t, "SYSNUMBER", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "loc", req.PickupLocation.Text)
	assert.Equal(t, "Page", req.RequestType.Text)
	assert.Equal(t, "Item", req.RequestScopeType.Text)

	response, err = ad.RequestItem("req1", "copynumber", "testuser", "loc", "itemloc")
	assert.NoError(t, err)
	req = mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Equal(t, "234.567", response.Barcode)
	assert.Equal(t, "QA123 .A45", response.CallNumber)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "copynumber", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	assert.Equal(t, "SYSNUMBER", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "loc", req.PickupLocation.Text)
	assert.Equal(t, "Page", req.RequestType.Text)
	assert.Equal(t, "Item", req.RequestScopeType.Text)

	response, err = ad.RequestItem("req1", "empty", "testuser", "loc", "itemloc")
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Empty(t, response.Barcode)
	assert.Empty(t, response.CallNumber)
	assert.Empty(t, response.Title)
	req = mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Equal(t, "testuser", req.UserId.UserIdentifierValue)
	assert.Equal(t, "empty", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	assert.Equal(t, "SYSNUMBER", req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "loc", req.PickupLocation.Text)
	assert.Equal(t, "Page", req.RequestType.Text)
	assert.Equal(t, "Item", req.RequestScopeType.Text)

	b = false
	ad = &LmsAdapterNcip{
		config: dirapi.LmsConfig{
			RequestItemPickupLocationEnabled: &b,
		},
		ncipClient: mock,
	}
	mock.(*ncipClientMock).lastRequest = nil
	response, err = ad.RequestItem("req1", "item1", "testuser", "pickloc", "")
	assert.NoError(t, err)
	assert.Equal(t, "123.456", response.Barcode)
	assert.Equal(t, "QA123 .A45", response.CallNumber)
	assert.Equal(t, "request title", response.Title)
	req = mock.(*ncipClientMock).lastRequest.(ncip.RequestItem)
	assert.Nil(t, req.PickupLocation)
	assert.Nil(t, req.ItemOptionalFields)

	mock.(*ncipClientMock).nilResponse = true
	_, err = ad.RequestItem("req1", "item2", "testuser", "loc", "itemloc")
	assert.Error(t, err)
	assert.Equal(t, "empty response from RequestItem", err.Error())

	disabled := false
	ad = &LmsAdapterNcip{
		config:     dirapi.LmsConfig{RequestItemEnabled: &disabled},
		ncipClient: mock,
	}
	mock.(*ncipClientMock).lastRequest = nil
	response, err = ad.RequestItem("", "item1", "testuser", "loc", "itemloc")
	assert.NoError(t, err)
	assert.Nil(t, response)
	assert.Nil(t, mock.(*ncipClientMock).lastRequest)

	ad = &LmsAdapterNcip{config: dirapi.LmsConfig{}, ncipClient: mock}
	_, err = ad.RequestItem("", "item1", "testuser", "loc", "itemloc")
	assert.EqualError(t, err, "missing request ID for RequestItem")
	assert.Nil(t, mock.(*ncipClientMock).lastRequest)
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
	assert.Equal(t, "Page", req.RequestType.Text)
	assert.Equal(t, "Item", req.RequestScopeType.Text)
}

func TestCheckInItem(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	b := true
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config: dirapi.LmsConfig{
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
		config: dirapi.LmsConfig{
			CheckOutItemEnabled: &b,
		},
	}
	mock.(*ncipClientMock).honorTitle = true
	ref := "extref"
	title, err := ad.CheckOutItem("req1", "item1", "barcodeid", ref)
	assert.NoError(t, err)
	assert.Equal(t, "fake title", title)
	req := mock.(*ncipClientMock).lastRequest.(ncip.CheckOutItem)
	assert.Equal(t, "req1", req.RequestId.RequestIdentifierValue)
	assert.Equal(t, "item1", req.ItemId.ItemIdentifierValue)
	assert.Equal(t, "barcodeid", req.UserId.UserIdentifierValue)
	assert.Equal(t, 1, len(req.ItemElementType))
	assert.Equal(t, "Bibliographic Description", req.ItemElementType[0].Text)
	bytes, err := xml.Marshal(ncip.RequestId{RequestIdentifierValue: ref})
	assert.NoError(t, err)
	assert.Equal(t, bytes, req.Ext.XMLContent)

	mock.(*ncipClientMock).honorTitle = false
	ref = "\x10" // will be replaced with replacement character
	title, err = ad.CheckOutItem("req1", "item1", "barcodeid", ref)
	assert.NoError(t, err)
	assert.Equal(t, "", title)
	req = mock.(*ncipClientMock).lastRequest.(ncip.CheckOutItem)
	bytes, err = xml.Marshal(ncip.RequestId{RequestIdentifierValue: ref})
	assert.NoError(t, err)
	assert.Equal(t, bytes, req.Ext.XMLContent)

	title, err = ad.CheckOutItem("", "item1", "barcodeid", "")
	assert.NoError(t, err)
	assert.Equal(t, "", title)
	req = mock.(*ncipClientMock).lastRequest.(ncip.CheckOutItem)
	assert.Nil(t, req.RequestId)

	mock.(*ncipClientMock).nilResponse = true
	_, err = ad.CheckOutItem("req1", "item1", "barcodeid", "extref")
	assert.Error(t, err)
	assert.Equal(t, "empty response from CheckOutItem", err.Error())

	b = false
	mock.(*ncipClientMock).lastRequest = nil
	title, err = ad.CheckOutItem("req1", "item1", "barcodeid", "extref")
	assert.NoError(t, err)
	assert.Equal(t, "", title)
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
	config := dirapi.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	institutionalPatron := ad.InstitutionalPatron("123456")
	assert.Equal(t, "INST-123456", institutionalPatron)

	p := "USER-{requesterSymbol}-XYZ"
	config = dirapi.LmsConfig{RequesterPatronPattern: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	institutionalPatron = ad.InstitutionalPatron("123456")
	assert.Equal(t, "USER-123456-XYZ", institutionalPatron)
}

func TestSupplierPickupLocation(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	config := dirapi.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation := ad.SupplierPickupLocation()
	assert.Equal(t, "ILL Office", pickupLocation)

	p := "Office2"
	config = dirapi.LmsConfig{SupplierPickupLocation: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation = ad.SupplierPickupLocation()
	assert.Equal(t, "Office2", pickupLocation)
}

func TestRequesterPickupLocation(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	config := dirapi.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation := ad.RequesterPickupLocation()
	assert.Equal(t, "Main Library", pickupLocation)

	p := "3rd Floor Desk"
	config = dirapi.LmsConfig{RequesterPickupLocation: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	pickupLocation = ad.RequesterPickupLocation()
	assert.Equal(t, "3rd Floor Desk", pickupLocation)
}

func TestItemLocation(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	config := dirapi.LmsConfig{}
	ad := &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	itemLocation := ad.ItemLocation()
	assert.Equal(t, "", itemLocation)

	p := "4"
	config = dirapi.LmsConfig{ItemLocation: &p}
	ad = &LmsAdapterNcip{
		ncipClient: mock,
		config:     config,
	}
	itemLocation = ad.ItemLocation()
	assert.Equal(t, "4", itemLocation)
}

func TestSetLogFunc(t *testing.T) {
	var mock ncipclient.NcipClient = new(ncipClientMock)
	ad := &LmsAdapterNcip{
		ncipClient: mock,
	}
	assert.Nil(t, mock.(*ncipClientMock).lastLogFunc)
	logFunc1 := func(outgoing map[string]any, incoming map[string]any, err error) {}
	ad.SetLogFunc(logFunc1)
	assert.NotNil(t, mock.(*ncipClientMock).lastLogFunc)
}

type ncipClientMock struct {
	lastRequest any
	honorTitle  bool
	nilResponse bool
	lastLogFunc ncipclient.NcipLogFunc
}

func (n *ncipClientMock) SetLogFunc(logFunc ncipclient.NcipLogFunc) {
	n.lastLogFunc = logFunc
}

func (n *ncipClientMock) LookupUser(lookup ncip.LookupUser) (*ncip.LookupUserResponse, error) {
	n.lastRequest = lookup
	if lookup.UserId != nil {
		if lookup.UserId.UserIdentifierValue == "staff-profile" {
			return lookupUserResponseWithProfile(lookup.UserId.UserIdentifierValue, "PROFILE", "STAFF", "Staff"), nil
		}
		if lookup.UserId.UserIdentifierValue == "blocked-profile" {
			return lookupUserResponseWithProfile(lookup.UserId.UserIdentifierValue, "PROFILE", "BLOCKED", "Blocked patrons"), nil
		}
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
	if lookup.AuthenticationInput[0].AuthenticationInputData == "blocked user" {
		return lookupUserResponseWithProfile("blocked-user-id", "BLOCKED", "Active", "Blocked patrons"), nil
	}
	if lookup.AuthenticationInput[0].AuthenticationInputData == "problem user" {
		return nil, &ncipclient.NcipError{
			Message: "NCIP user lookup failed",
			Problem: ncip.Problem{
				ProblemType:   ncip.SchemeValuePair{Text: string(ncip.UnknownUser)},
				ProblemDetail: "patron was not found",
			},
		}
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

func lookupUserResponseWithProfile(userID string, privilegeType string, status string, description string) *ncip.LookupUserResponse {
	return &ncip.LookupUserResponse{
		UserId: &ncip.UserId{UserIdentifierValue: userID},
		UserOptionalFields: &ncip.UserOptionalFields{
			UserPrivilege: []ncip.UserPrivilege{testUserPrivilege(privilegeType, status, description)},
		},
	}
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
	itemId := ""
	if len(request.BibliographicId) > 0 {
		itemId = request.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier
	}
	if itemId == "empty" {
		return &ncip.RequestItemResponse{}, nil
	}
	if n.nilResponse {
		return nil, nil
	}
	res := &ncip.RequestItemResponse{
		RequestId: &ncip.RequestId{RequestIdentifierValue: "lms-req-1"},
		ItemOptionalFields: &ncip.ItemOptionalFields{
			ItemDescription: &ncip.ItemDescription{
				CallNumber: "QA123 .A45",
				CopyNumber: "234.567",
			},
		},
	}
	if itemId != "copynumber" {
		res.ItemId = &ncip.ItemId{
			ItemIdentifierType:  &ncip.SchemeValuePair{Text: "Item Barcode"},
			ItemIdentifierValue: "123.456",
		}
	}
	for _, itemElement := range request.ItemElementType {
		if n.honorTitle && itemElement.Text == "Bibliographic Description" {
			res.ItemOptionalFields.BibliographicDescription = &ncip.BibliographicDescription{Title: "request title"}
			break
		}
	}
	return res, nil
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
	if n.nilResponse {
		return nil, nil
	}
	res := &ncip.CheckOutItemResponse{}
	for _, itemElement := range checkout.ItemElementType {
		if n.honorTitle && itemElement.Text == "Bibliographic Description" {
			res.ItemOptionalFields = &ncip.ItemOptionalFields{
				BibliographicDescription: &ncip.BibliographicDescription{
					Title: "fake title",
				},
			}
			break
		}
	}
	return res, nil
}

func (n *ncipClientMock) CreateUserFiscalTransaction(create ncip.CreateUserFiscalTransaction) (*ncip.CreateUserFiscalTransactionResponse, error) {
	n.lastRequest = create
	return nil, nil
}
