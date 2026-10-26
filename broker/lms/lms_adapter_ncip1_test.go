package lms

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/ncipclientv1"
	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
	ncip "github.com/indexdata/crosslink/ncipv1"
	"github.com/stretchr/testify/require"
)

type ncip1RoundTrip func(*http.Request) (*http.Response, error)

func (f ncip1RoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func ncip1TestAdapter(t *testing.T, response string, check func(ncip.NCIPMessage)) *LmsAdapterNcip1 {
	t.Helper()
	to := "lms"
	adapter := &LmsAdapterNcip1{config: dirapi.LmsConfig{Address: "https://lms.example/ncip", FromAgency: "broker", ToAgency: &to}}
	adapter.client = ncipclientv1.New(&http.Client{Transport: ncip1RoundTrip(func(r *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request ncip.NCIPMessage
		require.NoError(t, xml.Unmarshal(data, &request))
		require.Equal(t, ncip.NCIP_V1_0_DTD, request.Version)
		if validator, err := exec.LookPath("xmllint"); err == nil {
			cmd := exec.Command(validator, "--nonet", "--noout", "--schema", "../../ncipv1/xsd/ncip_v1_0.xsd", "-")
			cmd.Stdin = bytes.NewReader(data)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, string(output))
		}
		if check != nil {
			check(request)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/xml"}}, Body: io.NopCloser(strings.NewReader("<NCIPMessage>" + response + "</NCIPMessage>"))}, nil
	})}, adapter.config.Address)
	return adapter
}

func TestNcip1Operations(t *testing.T) {
	a := ncip1TestAdapter(t, `<AcceptItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId></AcceptItemResponse>`, func(m ncip.NCIPMessage) {
		r := m.AcceptItem
		require.NotNil(t, r)
		require.Equal(t, "pickup", r.InitiationHeader.ToAgencyId.UniqueAgencyId.Value)
		require.Equal(t, "lms", r.UniqueUserId.UniqueAgencyId.Value)
		require.Equal(t, "broker", r.UniqueItemId.UniqueAgencyId.Value)
		require.Equal(t, "broker", r.UniqueRequestId.UniqueAgencyId.Value)
		require.Equal(t, "Hold For Pickup", r.RequestedActionType.Value)
		require.NotNil(t, r.IndeterminateLoanPeriodFlag)
		require.Equal(t, "isbn", r.ItemOptionalFields.BibliographicDescription.BibliographicItemId.BibliographicItemIdentifier)
		require.Equal(t, "call", r.ItemOptionalFields.ItemDescription.CallNumber)
	})
	logged := 0
	a.SetLogFunc(func(map[string]any, map[string]any, error) { logged++ })
	accepted, err := a.AcceptItem("item", "request", "user", "author", "title", "isbn", "call", "pickup", "")
	require.NoError(t, err)
	require.NotNil(t, accepted)
	require.Equal(t, 1, logged)
	a = ncip1TestAdapter(t, `<RequestItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>lms-request</RequestIdentifierValue></UniqueRequestId><UniqueItemId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><ItemIdentifierValue>internal-id</ItemIdentifierValue></UniqueItemId><UniqueUserId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><UserIdentifierValue>user</UserIdentifierValue></UniqueUserId><RequestType><Scheme>urn:test:type</Scheme><Value>Page</Value></RequestType><RequestScopeType><Scheme>urn:test:scope</Scheme><Value>Item</Value></RequestScopeType><ItemOptionalFields><BibliographicDescription><Title>Title</Title></BibliographicDescription><ItemDescription><VisibleItemId><VisibleItemIdentifierType><Value>Item Barcode</Value></VisibleItemIdentifierType><VisibleItemIdentifier>barcode</VisibleItemIdentifier></VisibleItemId><CallNumber>call</CallNumber><CopyNumber>copy</CopyNumber></ItemDescription><Location><LocationName><LocationNameInstance><LocationNameLevel>1</LocationNameLevel><LocationNameValue>Main</LocationNameValue></LocationNameInstance><LocationNameInstance><LocationNameLevel>2</LocationNameLevel><LocationNameValue>Stacks</LocationNameValue></LocationNameInstance></LocationName></Location></ItemOptionalFields></RequestItemResponse>`, func(m ncip.NCIPMessage) {
		r := m.RequestItem
		require.NotNil(t, r)
		require.Equal(t, "123", r.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifier)
		require.Equal(t, "request", r.UniqueRequestId.RequestIdentifierValue)
		require.Equal(t, "Page", r.RequestType.Value)
	})
	normalization := "sierra"
	a.config.BibIdNormalization = &normalization
	item, err := a.RequestItem("request", ".b1234", "user", "ignored", "")
	require.NoError(t, err)
	require.Equal(t, &RequestedItem{RequestID: encodeNcip1RequestID(ncip.UniqueRequestId{UniqueAgencyId: ncip.UniqueAgencyId{Scheme: "urn:test:agency", Value: "lms"}, RequestIdentifierValue: "lms-request"}), ItemID: encodeNcip1ItemID(ncip.UniqueItemId{UniqueAgencyId: ncip.UniqueAgencyId{Scheme: "urn:test:agency", Value: "lms"}, ItemIdentifierValue: "internal-id"}), Title: "Title", Barcode: "barcode", CallNumber: "call", Location: "Main", ShelvingLocation: "Stacks"}, item)
	a = ncip1TestAdapter(t, `<CancelRequestItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId><UniqueUserId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><UserIdentifierValue>user</UserIdentifierValue></UniqueUserId></CancelRequestItemResponse>`, func(m ncip.NCIPMessage) {
		require.Equal(t, "request", m.CancelRequestItem.UniqueRequestId.RequestIdentifierValue)
		require.Nil(t, m.CancelRequestItem.UniqueItemId)
	})
	require.NoError(t, a.CancelRequestItem("request", "user"))
	a = ncip1TestAdapter(t, `<CheckInItemResponse>`+ncip1TestHeader+`<UniqueItemId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><ItemIdentifierValue>barcode</ItemIdentifierValue></UniqueItemId></CheckInItemResponse>`, func(m ncip.NCIPMessage) { require.Equal(t, "lms", m.CheckInItem.UniqueItemId.UniqueAgencyId.Value) })
	performed, err := a.CheckInItem("barcode")
	require.NoError(t, err)
	require.True(t, performed)
	for _, date := range []string{"2026-12-01T12:00:00Z", "bad", "0000-01-01T00:00:00Z", ""} {
		a = ncip1TestAdapter(t, `<CheckOutItemResponse>`+ncip1TestHeader+ncip1TestItem+ncip1TestUser+`<DateDue>`+date+`</DateDue><ItemOptionalFields><BibliographicDescription><Title>Title</Title></BibliographicDescription></ItemOptionalFields></CheckOutItemResponse>`, func(m ncip.NCIPMessage) {
			require.Equal(t, "request", m.CheckOutItem.UniqueRequestId.RequestIdentifierValue)
			require.Equal(t, "barcode", m.CheckOutItem.UniqueItemId.ItemIdentifierValue)
		})
		item, err := a.CheckOutItem("request", "barcode", "user", "")
		require.NoError(t, err)
		require.Equal(t, "Title", item.Title)
		if date == "2026-12-01T12:00:00Z" {
			require.NotNil(t, item.DueDate)
		} else {
			require.Nil(t, item.DueDate)
		}
	}
}

func TestNcip1Lookup(t *testing.T) {
	response := `<LookupUserResponse>` + ncip1TestHeader + `<UniqueUserId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><UserIdentifierValue>canonical</UserIdentifierValue></UniqueUserId><UserOptionalFields><NameInformation><PersonalNameInformation><StructuredPersonalUserName><GivenName> First </GivenName><Surname> Last </Surname></StructuredPersonalUserName></PersonalNameInformation></NameInformation><UserAddressInformation><ElectronicAddress><ElectronicAddressType><Value>Email</Value></ElectronicAddressType><ElectronicAddressData>a@example.org</ElectronicAddressData></ElectronicAddress></UserAddressInformation><UserAddressInformation><ElectronicAddress><ElectronicAddressType><Value>mailto</Value></ElectronicAddressType><ElectronicAddressData>A@example.org</ElectronicAddressData></ElectronicAddress></UserAddressInformation><UserPrivilege><AgencyUserPrivilegeType><Value>PROFILE</Value></AgencyUserPrivilegeType><UserPrivilegeStatus><UserPrivilegeStatusType><Value>STAFF</Value></UserPrivilegeStatusType></UserPrivilegeStatus></UserPrivilege></UserOptionalFields></LookupUserResponse>`
	a := ncip1TestAdapter(t, response, func(m ncip.NCIPMessage) {
		require.Equal(t, "user", m.LookupUser.UniqueUserId.UserIdentifierValue)
		require.Len(t, m.LookupUser.UserElementType, 4)
	})
	result, err := a.LookupUser("user", LookupUserOptions{IncludePatronInfo: true, ValidatePatronProfile: true})
	require.NoError(t, err)
	require.Equal(t, LookupUserResult{UserID: "canonical", GivenName: "First", Surname: "Last", EmailAddresses: []string{"a@example.org"}}, result)

	code := "STAFF"
	a.config.PatronProfiles = &dirapi.PatronProfiles{{Code: &code, CanCreateRequests: false}}
	_, err = a.LookupUser("user", LookupUserOptions{IncludePatronInfo: true, ValidatePatronProfile: true})
	var denied *PatronProfileIneligibleError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, "STAFF", denied.ProfileCode)
	a = ncip1TestAdapter(t, `<LookupUserResponse>`+ncip1TestHeader+`<UniqueUserId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><UserIdentifierValue>user</UserIdentifierValue></UniqueUserId></LookupUserResponse>`, nil)
	result, err = a.LookupUser("user", LookupUserOptions{})
	require.NoError(t, err)
	require.Equal(t, "user", result.UserID)
	// First a protocol rejection, then a barcode query returning the canonical ID.
	count := 0
	a = ncip1TestAdapter(t, response, func(m ncip.NCIPMessage) {
		count++
		require.Nil(t, m.LookupUser.UniqueUserId)
		require.Equal(t, "Barcode Id", m.LookupUser.AuthenticationInput[0].AuthenticationInputType.Value)
	})
	success := a.client
	reject := ncip1TestAdapter(t, `<LookupUserResponse>`+ncip1TestHeader+`<Problem><ProcessingError><ProcessingErrorType><Scheme>urn:test:error</Scheme><Value>Unknown User</Value></ProcessingErrorType><ProcessingErrorElement><ElementName>UniqueUserId</ElementName></ProcessingErrorElement></ProcessingError></Problem></LookupUserResponse>`, func(ncip.NCIPMessage) { a.client = success })
	a.client = reject.client
	result, err = a.LookupUser("barcode", LookupUserOptions{})
	require.NoError(t, err)
	require.Equal(t, "canonical", result.UserID)
	require.Equal(t, 1, count)
}

func TestNcip1UnsupportedAndDisabled(t *testing.T) {
	a := ncip1TestAdapter(t, "", func(ncip.NCIPMessage) { t.Fatal("unexpected request") })
	performed, err := a.DeleteItem("item")
	require.ErrorIs(t, err, ErrDeleteItemUnsupported)
	require.False(t, performed)
	require.Error(t, a.CreateUserFiscalTransaction("user", "item"))
	_, err = a.RequestItem("request", "bib", "user", "", "location")
	require.Error(t, err)
	_, err = a.CheckOutItem("request", "item", "user", "external")
	require.Error(t, err)
	_, err = a.LookupUser("", LookupUserOptions{})
	require.Error(t, err)
	_, err = a.AcceptItem("", "request", "user", "", "", "", "", "", "")
	require.Error(t, err)
	_, err = a.RequestItem("", "bib", "user", "", "")
	require.Error(t, err)
	_, err = a.CheckOutItem("", "", "user", "")
	require.Error(t, err)
	_, err = a.CheckInItem("")
	require.Error(t, err)
	require.Error(t, a.CancelRequestItem("", "user"))
	disabled := false
	a.config.AcceptItemEnabled = &disabled
	a.config.RequestItemEnabled = &disabled
	a.config.LookupUserEnabled = &disabled
	a.config.CheckInItemEnabled = &disabled
	a.config.CheckOutItemEnabled = &disabled
	accepted, err := a.AcceptItem("", "", "", "", "", "", "", "", "")
	require.NoError(t, err)
	require.Nil(t, accepted)
	performed, err = a.DeleteItem("")
	require.NoError(t, err)
	require.False(t, performed)
	performed, err = a.CheckInItem("")
	require.NoError(t, err)
	require.False(t, performed)
	item, err := a.RequestItem("", "", "", "", "")
	require.NoError(t, err)
	require.Nil(t, item)
	checkout, err := a.CheckOutItem("", "", "", "")
	require.NoError(t, err)
	require.Nil(t, checkout)
	user, err := a.LookupUser("patron", LookupUserOptions{})
	require.NoError(t, err)
	require.Equal(t, "patron", user.UserID)
	require.False(t, a.AcceptItemUsesPickupLocation())
	require.False(t, a.RequestItemUsesPickupLocation())
}

func TestNcip1Configuration(t *testing.T) {
	_, err := CreateLmsAdapterNcip1(dirapi.LmsConfig{})
	require.Error(t, err)
	adapter, err := CreateLmsAdapterNcip1(dirapi.LmsConfig{Address: "https://lms.example", FromAgency: "broker"})
	require.NoError(t, err)
	a := adapter.(*LmsAdapterNcip1)
	require.Equal(t, "default-to-agency", a.toAgency())
	require.Equal(t, "INST-US-TEST", a.InstitutionalPatron("us-test"))
	require.Equal(t, "ILL Office", a.SupplierPickupLocation())
	require.Equal(t, "Main Library", a.RequesterPickupLocation())
	require.Empty(t, a.ItemLocation())
	require.True(t, a.AcceptItemUsesPickupLocation())
	require.False(t, a.RequestItemUsesPickupLocation())
	pickup, location, patron, auth := "pickup", "location", "P-{requesterSymbol}", "secret"
	a.config.SupplierPickupLocation = &pickup
	a.config.RequesterPickupLocation = &pickup
	a.config.ItemLocation = &location
	a.config.RequesterPatronPattern = &patron
	a.config.FromAgencyAuthentication = &auth
	require.Equal(t, pickup, a.SupplierPickupLocation())
	require.Equal(t, pickup, a.RequesterPickupLocation())
	require.Equal(t, location, a.ItemLocation())
	require.Equal(t, "P-US-TEST", a.InstitutionalPatron("us-test"))
	require.Equal(t, auth, a.header().FromAgencyAuthentication)
	requestType, scope, code := "Hold", "Bibliographic Item", "local"
	a = ncip1TestAdapter(t, `<RequestItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId><UniqueUserId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><UserIdentifierValue>user</UserIdentifierValue></UniqueUserId><RequestType><Scheme>urn:test:type</Scheme><Value>Page</Value></RequestType><RequestScopeType><Scheme>urn:test:scope</Scheme><Value>Item</Value></RequestScopeType><ItemOptionalFields><ItemDescription><CopyNumber>barcode</CopyNumber></ItemDescription><Location><LocationName><LocationNameInstance><LocationNameLevel>1</LocationNameLevel><LocationNameValue>Main</LocationNameValue></LocationNameInstance></LocationName></Location></ItemOptionalFields></RequestItemResponse>`, func(m ncip.NCIPMessage) {
		require.Equal(t, requestType, m.RequestItem.RequestType.Value)
		require.Equal(t, scope, m.RequestItem.RequestScopeType.Value)
		require.Equal(t, code, m.RequestItem.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifierCode.Value)
	})
	a.config.RequestItemRequestType = &requestType
	a.config.RequestItemRequestScopeType = &scope
	a.config.RequestItemBibIdCode = &code
	item, err := a.RequestItem("request", "bib", "user", "", "")
	require.NoError(t, err)
	decoded, err := a.savedRequest(item.RequestID)
	require.NoError(t, err)
	require.Equal(t, "request", decoded.RequestIdentifierValue)
	require.Equal(t, "lms", decoded.UniqueAgencyId.Value)
	require.Equal(t, "barcode", item.Barcode)
	require.Equal(t, "Main", item.ShelvingLocation)
}

func TestNcip1Failures(t *testing.T) {
	problem := `<Problem><ProcessingError><ProcessingErrorType><Scheme>urn:test:error</Scheme><Value>Rejected</Value></ProcessingErrorType><ProcessingErrorElement><ElementName>UniqueUserId</ElementName></ProcessingErrorElement></ProcessingError></Problem>`
	a := ncip1TestAdapter(t, `<LookupUserResponse>`+ncip1TestHeader+``+problem+`</LookupUserResponse>`, nil)
	_, err := a.LookupUser("user", LookupUserOptions{})
	require.Error(t, err)
	a = ncip1TestAdapter(t, `<RequestItemResponse>`+ncip1TestHeader+``+problem+`</RequestItemResponse>`, nil)
	item, err := a.RequestItem("request", "bib", "user", "", "")
	require.Error(t, err)
	require.Nil(t, item)
	a = ncip1TestAdapter(t, `<CheckOutItemResponse>`+ncip1TestHeader+``+problem+`</CheckOutItemResponse>`, nil)
	checkout, err := a.CheckOutItem("", "item", "user", "")
	require.Error(t, err)
	require.Nil(t, checkout)
	a = ncip1TestAdapter(t, `<AcceptItemResponse>`+ncip1TestHeader+``+problem+`</AcceptItemResponse>`, nil)
	accepted, err := a.AcceptItem("item", "request", "user", "", "", "", "", "", "")
	require.Error(t, err)
	require.Nil(t, accepted)
}

const ncip1TestHeader = `<ResponseHeader><FromAgencyId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId></FromAgencyId><ToAgencyId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId></ToAgencyId></ResponseHeader>`
const ncip1TestItem = `<UniqueItemId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><ItemIdentifierValue>barcode</ItemIdentifierValue></UniqueItemId>`
const ncip1TestUser = `<UniqueUserId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><UserIdentifierValue>canonical</UserIdentifierValue></UniqueUserId>`

func TestNcip1SavedRequestReferences(t *testing.T) {
	a := &LmsAdapterNcip1{config: dirapi.LmsConfig{FromAgency: "broker"}}
	original := ncip.UniqueRequestId{UniqueAgencyId: ncip.UniqueAgencyId{Scheme: "urn:test:agency/é", Value: "lms.with:punctuation"}, RequestIdentifierValue: " request:&/é "}
	decoded, err := a.savedRequest(encodeNcip1RequestID(original))
	require.NoError(t, err)
	require.Equal(t, original, *decoded)
	legacy, err := a.savedRequest("legacy-request")
	require.NoError(t, err)
	require.Equal(t, "broker", legacy.UniqueAgencyId.Value)
	require.Equal(t, "legacy-request", legacy.RequestIdentifierValue)
	for _, id := range []string{"ncip1-request:", "ncip1-request:a.b", "ncip1-request:!.YQ.Yg", "ncip1-request:YQ..Yg", "ncip1-request:YQ.Yg.IA"} {
		// No client is installed: malformed references must fail before any HTTP call.
		require.ErrorContains(t, a.CancelRequestItem(id, "user"), "invalid NCIP v1 request reference")
		_, err := a.CheckOutItem(id, "item", "user", "")
		require.ErrorContains(t, err, "invalid NCIP v1 request reference")
	}
}

func TestNcip1MalformedItemReferences(t *testing.T) {
	a := &LmsAdapterNcip1{}
	for _, id := range []string{"ncip1-item:", "ncip1-item:a.b", "ncip1-item:!.YQ.Yg", "ncip1-item:YQ..Yg", "ncip1-item:YQ.Yg.IA"} {
		// No client: these references must fail before sending an HTTP request.
		_, err := a.CheckOutItem("", id, "user", "")
		require.ErrorContains(t, err, "invalid NCIP v1 item reference")
		_, err = a.CheckInItem(id)
		require.ErrorContains(t, err, "invalid NCIP v1 item reference")
	}
}

func TestNcip1ResolvedSierraRequestScope(t *testing.T) {
	var entry dirapi.Entry
	require.NoError(t, json.Unmarshal([]byte(`{"lmsConfig":{"vendor":"Sierra","ncipVersion":"1.0","address":"https://lms.example/ncip","fromAgency":"broker"},"catalogConfig":{"zoom":{"address":"catalog:210"}}}`), &entry))
	effective, err := profiles.Resolve(entry)
	require.NoError(t, err)
	require.Equal(t, "Sierra", effective.Origins["lmsConfig.requestItemRequestScopeType"])
	require.Equal(t, "Title", *effective.LMS.RequestItemRequestScopeType)
	adapter, err := CreateLmsAdapterNcip(*effective.LMS)
	require.NoError(t, err)
	a, ok := adapter.(*LmsAdapterNcip1)
	require.True(t, ok)
	expected := ncip.RequestScopeType{Scheme: ncip1Scheme + "imp1/schemes/requestscopetype/requestscopetype.scm", Value: "Bibliographic Item"}
	requestID := `<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId>`
	request := ncip1TestAdapter(t, "<RequestItemResponse>"+ncip1TestHeader+requestID+ncip1TestUser+
		`<RequestType><Scheme>urn:test:type</Scheme><Value>Hold</Value></RequestType><RequestScopeType><Scheme>urn:test:scope</Scheme><Value>Bibliographic Item</Value></RequestScopeType></RequestItemResponse>`, func(m ncip.NCIPMessage) {
		require.NotNil(t, m.RequestItem)
		require.Equal(t, expected.Scheme, m.RequestItem.RequestScopeType.Scheme)
		require.Equal(t, expected.Value, m.RequestItem.RequestScopeType.Value)
	})
	a.client = request.client
	item, err := a.RequestItem("request", "bib", "user", "", "")
	require.NoError(t, err)
	require.NotNil(t, item)
	cancel := ncip1TestAdapter(t, "<CancelRequestItemResponse>"+ncip1TestHeader+requestID+ncip1TestUser+"</CancelRequestItemResponse>", func(m ncip.NCIPMessage) {
		require.NotNil(t, m.CancelRequestItem)
		require.NotNil(t, m.CancelRequestItem.RequestScopeType)
		require.Equal(t, expected.Scheme, m.CancelRequestItem.RequestScopeType.Scheme)
		require.Equal(t, expected.Value, m.CancelRequestItem.RequestScopeType.Value)
	})
	a.client = cancel.client
	require.NoError(t, a.CancelRequestItem(item.RequestID, "user"))
	require.Equal(t, "Title", *effective.LMS.RequestItemRequestScopeType, "translation must not mutate shared profile values")
	require.Equal(t, "Title", (&LmsAdapterNcip{config: *effective.LMS}).requestItemRequestScopeType())
}

func TestAdditionalProfileNcip1Request(t *testing.T) {
	for _, vendor := range []string{"Horizon", "Polaris", "Symphony"} {
		t.Run(vendor, func(t *testing.T) {
			a := ncip1TestAdapter(t, `<RequestItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId>`+ncip1TestUser+`<RequestType><Scheme>urn:test:type</Scheme><Value>Loan</Value></RequestType><RequestScopeType><Scheme>urn:test:scope</Scheme><Value>Bibliographic Item</Value></RequestScopeType></RequestItemResponse>`, func(m ncip.NCIPMessage) {
				require.NotNil(t, m.RequestItem)
				r := m.RequestItem
				want := ".b12345"
				if vendor == "Symphony" {
					want = "12345"
				}
				require.Equal(t, want, r.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifier)
				require.Equal(t, "SYSNUMBER", r.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifierCode.Value)
				require.Equal(t, "Loan", r.RequestType.Value)
				require.Equal(t, "Bibliographic Item", r.RequestScopeType.Value)
			})
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(`{"lmsConfig":{"vendor":"`+vendor+`","address":"https://lms.example/ncip","fromAgency":"broker","toAgency":"lms"}}`), &entry))
			e, err := profiles.Resolve(entry)
			require.NoError(t, err)
			a.config = *e.LMS
			_, err = a.RequestItem("request", ".b12345", "user", "ignored", "")
			require.NoError(t, err)
		})
	}
}
