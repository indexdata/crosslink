package ncipclient

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"

	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/ncip"

	"github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
)

func TestMain(m *testing.M) {
	mockPort := utils.Must(test.GetFreePort())

	apputils.StartMockApp(mockPort)

	m.Run()
}

func TestPrepareHeaderEmpty(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipData := make(map[string]any)

	header := ncipClient.prepareHeader(ncipData, nil)
	assert.Equal(t, "default-from-agency", header.FromAgencyId.AgencyId.Text)
	assert.Equal(t, "default-to-agency", header.ToAgencyId.AgencyId.Text)
	assert.Equal(t, "", header.FromAgencyAuthentication)
}

func TestPrepareHeaderValues(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipData := make(map[string]any)
	ncipData["to_agency"] = "ILL-MOCK1"
	ncipData["from_agency"] = "ILL-MOCK2"
	ncipData["from_agency_authentication"] = "pass"

	header := ncipClient.prepareHeader(ncipData, nil)
	assert.Equal(t, "ILL-MOCK1", header.ToAgencyId.AgencyId.Text)
	assert.Equal(t, "ILL-MOCK2", header.FromAgencyId.AgencyId.Text)
	assert.Equal(t, "pass", header.FromAgencyAuthentication)
}

func TestLookupUserAutoOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.LookupUser(customData, lookup)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestLookupUserAutoInvalidUser(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "foo",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "NCIP user lookup failed: Unknown User: foo", err.Error())
}

func TestLookupUserModeManual(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "manual"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.LookupUser(customData, lookup)
	assert.NoError(t, err)
	assert.False(t, b)
}

func TestLookupUserModeDisabled(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "disabled"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.LookupUser(customData, lookup)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestLookupUserMissingAddress(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in customData", err.Error())
}

func TestLookupUserMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	lookup := ncip.LookupUser{}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestLookupUserMissingAuthUserInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "missing lookup_user_mode in ncip configuration", err.Error())
}

func TestLookupUserBadMode(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "foo"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "unknown value for lookup_user_mode: foo", err.Error())
}

func TestBadNcipMessageResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("<myType><msg>OK</msg></myType>"))
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["accept_item_mode"] = "auto"
	ncipData["request_item_mode"] = "auto"
	ncipData["create_user_fiscal_transaction_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = server.URL
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	accept := ncip.AcceptItem{
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	_, err = ncipClient.AcceptItem(customData, accept)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	delete := ncip.DeleteItem{}
	err = ncipClient.DeleteItem(customData, delete)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	request := ncip.RequestItem{}
	_, err = ncipClient.RequestItem(customData, request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	cancelRequest := ncip.CancelRequestItem{}
	err = ncipClient.CancelRequestItem(customData, cancelRequest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	checkInItem := ncip.CheckInItem{}
	err = ncipClient.CheckInItem(customData, checkInItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	checkOutItem := ncip.CheckOutItem{}
	err = ncipClient.CheckOutItem(customData, checkOutItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	createUserFiscalTransaction := ncip.CreateUserFiscalTransaction{}
	_, err = ncipClient.CreateUserFiscalTransaction(customData, createUserFiscalTransaction)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")
}

func TestEmptyNcipResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		var ncipResponse = ncip.NCIPMessage{
			Version: ncip.NCIP_V2_02_XSD,
		}
		bytesResponse, err := xml.Marshal(ncipResponse)
		if err != nil {
			http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		netutil.WriteHttpResponse(w, bytesResponse)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["accept_item_mode"] = "auto"
	ncipData["request_item_mode"] = "auto"
	ncipData["create_user_fiscal_transaction_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = server.URL
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "invalid NCIP response: missing LookupUserResponse", err.Error())

	accept := ncip.AcceptItem{
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	_, err = ncipClient.AcceptItem(customData, accept)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing AcceptItemResponse")

	delete := ncip.DeleteItem{}
	err = ncipClient.DeleteItem(customData, delete)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing DeleteItemResponse")

	request := ncip.RequestItem{}
	_, err = ncipClient.RequestItem(customData, request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing RequestItemResponse")

	cancelRequest := ncip.CancelRequestItem{}
	err = ncipClient.CancelRequestItem(customData, cancelRequest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CancelRequestItemResponse")

	checkInItem := ncip.CheckInItem{}
	err = ncipClient.CheckInItem(customData, checkInItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CheckInItemResponse")

	checkOutItem := ncip.CheckOutItem{}
	err = ncipClient.CheckOutItem(customData, checkOutItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CheckOutItemResponse")

	createUserFiscalTransaction := ncip.CreateUserFiscalTransaction{}
	_, err = ncipClient.CreateUserFiscalTransaction(customData, createUserFiscalTransaction)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CreateUserFiscalTransactionResponse")
}

func setProblem(msg ncip.ProblemTypeMessage, detail string) []ncip.Problem {
	return []ncip.Problem{
		{
			ProblemType:   ncip.SchemeValuePair{Text: string(msg)},
			ProblemDetail: detail,
		},
	}
}

func TestLookupUserProblemResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		msg := "Some Problem"
		var ncipResponse = ncip.NCIPMessage{
			Version: ncip.NCIP_V2_02_XSD,
			Problem: setProblem(ncip.ProblemTypeMessage(msg), "Details about the problem"),
		}
		bytesResponse, err := xml.Marshal(ncipResponse)
		if err != nil {
			http.Error(w, "marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		netutil.WriteHttpResponse(w, bytesResponse)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = server.URL
	customData["ncip"] = ncipData

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "NCIP message processing failed: Some Problem: Details about the problem", err.Error())
}

func TestAcceptItemOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["accept_item_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	accept := ncip.AcceptItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	b, err := ncipClient.AcceptItem(customData, accept)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestAcceptItemInvalidUser(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["accept_item_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	accept := ncip.AcceptItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "foo",
		},
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	_, err := ncipClient.AcceptItem(customData, accept)
	assert.Error(t, err)
	assert.Equal(t, "NCIP accept item failed: Unknown User: foo", err.Error())
}

func TestAcceptItemModeManual(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["accept_item_mode"] = "manual"
	customData["ncip"] = ncipData

	lookup := ncip.AcceptItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	b, err := ncipClient.AcceptItem(customData, lookup)
	assert.NoError(t, err)
	assert.False(t, b)
}

func TestAcceptItemMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	accept := ncip.AcceptItem{}
	_, err := ncipClient.AcceptItem(customData, accept)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestDeleteItemOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	delete := ncip.DeleteItem{}
	err := ncipClient.DeleteItem(customData, delete)
	assert.NoError(t, err)
}

func TestDeleteItemMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	delete := ncip.DeleteItem{}
	err := ncipClient.DeleteItem(customData, delete)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestRequestItemOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["request_item_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	request := ncip.RequestItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		ItemId: []ncip.ItemId{{
			ItemIdentifierValue: "item-001",
		}},
		RequestScopeType: ncip.SchemeValuePair{
			Text: "Bibliographic",
		},
		RequestType: ncip.SchemeValuePair{
			Text: "Hold",
		},
	}
	b, err := ncipClient.RequestItem(customData, request)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestRequestItemModeManual(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["request_item_mode"] = "manual"
	customData["ncip"] = ncipData

	lookup := ncip.RequestItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	b, err := ncipClient.RequestItem(customData, lookup)
	assert.NoError(t, err)
	assert.False(t, b)
}

func TestRequestItemMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	request := ncip.RequestItem{}
	_, err := ncipClient.RequestItem(customData, request)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestCancelRequestItemOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	request := ncip.CancelRequestItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		ItemId: &ncip.ItemId{
			ItemIdentifierValue: "item-001",
		},
		RequestType: ncip.SchemeValuePair{
			Text: "Hold",
		},
	}
	err := ncipClient.CancelRequestItem(customData, request)
	assert.NoError(t, err)
}

func TestCancelRequestItemMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	request := ncip.CancelRequestItem{}
	err := ncipClient.CancelRequestItem(customData, request)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestCheckInItemOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	request := ncip.CheckInItem{
		ItemId: ncip.ItemId{
			ItemIdentifierValue: "item-001",
		},
	}
	err := ncipClient.CheckInItem(customData, request)
	assert.NoError(t, err)
}

func TestCheckInItemMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	request := ncip.CheckInItem{}
	err := ncipClient.CheckInItem(customData, request)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestCheckOutItemOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	request := ncip.CheckOutItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		ItemId: ncip.ItemId{
			ItemIdentifierValue: "item-001",
		},
	}
	err := ncipClient.CheckOutItem(customData, request)
	assert.NoError(t, err)
}

func TestCheckOutItemMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	request := ncip.CheckOutItem{}
	err := ncipClient.CheckOutItem(customData, request)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}

func TestCreateUserFiscalTransactionOK(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["create_user_fiscal_transaction_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	customData["ncip"] = ncipData

	lookup := ncip.CreateUserFiscalTransaction{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		FiscalTransactionInformation: ncip.FiscalTransactionInformation{
			FiscalActionType: ncip.SchemeValuePair{Text: "Charge"},
			FiscalTransactionReferenceId: &ncip.FiscalTransactionReferenceId{
				FiscalTransactionIdentifierValue: "ft-001",
			},
		},
	}
	b, err := ncipClient.CreateUserFiscalTransaction(customData, lookup)
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestCreateUserFiscalTransactionBadMode(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)

	customData := make(map[string]any)
	ncipData := make(map[string]any)
	ncipData["create_user_fiscal_transaction_mode"] = "foo"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"
	customData["ncip"] = ncipData

	lookup := ncip.CreateUserFiscalTransaction{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.CreateUserFiscalTransaction(customData, lookup)
	assert.Error(t, err)
	assert.Equal(t, "unknown value for create_user_fiscal_transaction_mode: foo", err.Error())
}

func TestCreateUserFiscalTransactionMissingNcipInfo(t *testing.T) {
	ncipClient := CreateNcipClient(http.DefaultClient)
	customData := make(map[string]any)
	request := ncip.CreateUserFiscalTransaction{}
	_, err := ncipClient.CreateUserFiscalTransaction(customData, request)
	assert.Error(t, err)
	assert.Equal(t, "missing ncip configuration in customData", err.Error())
}
