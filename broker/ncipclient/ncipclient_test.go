package ncipclient

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"

	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/ncip"

	test "github.com/indexdata/crosslink/broker/test/utils"
)

func TestMain(m *testing.M) {
	mockPort := utils.Must(test.GetFreePort())

	// same as apputils.StartMockApp
	// but not used here as this results in cyclic dependencies
	test.Expect(os.Setenv("HTTP_PORT", strconv.Itoa(mockPort)), "failed to set mock server port")
	go func() {
		var mockApp mockapp.MockApp
		test.Expect(mockApp.Run(), "failed to start illmock server")
	}()
	test.WaitForServiceUp(mockPort)

	os.Exit(m.Run())
}

func TestPrepareHeaderEmpty(t *testing.T) {
	ncipData := make(map[string]any)
	ncipClient := NcipClientImpl{}
	ncipClient.ncipInfo = ncipData

	header := ncipClient.prepareHeader(nil)
	assert.Equal(t, "default-from-agency", header.FromAgencyId.AgencyId.Text)
	assert.Equal(t, "default-to-agency", header.ToAgencyId.AgencyId.Text)
	assert.Equal(t, "", header.FromAgencyAuthentication)
}

func TestPrepareHeaderValues(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipData := make(map[string]any)
	ncipClient.ncipInfo = ncipData

	ncipData["to_agency"] = "ILL-MOCK1"
	ncipData["from_agency"] = "ILL-MOCK2"
	ncipData["from_agency_authentication"] = "pass"

	header := ncipClient.prepareHeader(nil)
	assert.Equal(t, "ILL-MOCK1", header.ToAgencyId.AgencyId.Text)
	assert.Equal(t, "ILL-MOCK2", header.FromAgencyId.AgencyId.Text)
	assert.Equal(t, "pass", header.FromAgencyAuthentication)
}

func TestLookupUserAutoOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	res, err := ncipClient.LookupUser(lookup)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestLookupUserAutoInvalidUser(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "foo",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "NCIP user lookup failed: Unknown User: foo", err.Error())
}

func TestLookupUserModeManual(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "manual"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	res, err := ncipClient.LookupUser(lookup)
	assert.NoError(t, err)
	assert.Nil(t, res)
}

func TestLookupUserModeDisabled(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "disabled"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	res, err := ncipClient.LookupUser(lookup)
	assert.NoError(t, err)
	assert.Nil(t, res)
}

func TestLookupUserMissingAddress(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	res, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in configuration", err.Error())
	assert.Nil(t, res)
}

func TestLookupUserMissingAuthUserInfo(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "missing lookup_user_mode in NCIP configuration", err.Error())
}

func TestLookupUserBadMode(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "foo"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
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

	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["accept_item_mode"] = "auto"
	ncipData["request_item_mode"] = "auto"
	ncipData["create_user_fiscal_transaction_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = server.URL

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	accept := ncip.AcceptItem{
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	_, err = ncipClient.AcceptItem(accept)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	delete := ncip.DeleteItem{}
	_, err = ncipClient.DeleteItem(delete)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	request := ncip.RequestItem{}
	_, err = ncipClient.RequestItem(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	cancelRequest := ncip.CancelRequestItem{}
	_, err = ncipClient.CancelRequestItem(cancelRequest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	checkInItem := ncip.CheckInItem{}
	_, err = ncipClient.CheckInItem(checkInItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	checkOutItem := ncip.CheckOutItem{}
	_, err = ncipClient.CheckOutItem(checkOutItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NCIP message exchange failed:")

	createUserFiscalTransaction := ncip.CreateUserFiscalTransaction{}
	_, err = ncipClient.CreateUserFiscalTransaction(createUserFiscalTransaction)
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

	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["accept_item_mode"] = "auto"
	ncipData["request_item_mode"] = "auto"
	ncipData["create_user_fiscal_transaction_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = server.URL

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "invalid NCIP response: missing LookupUserResponse", err.Error())

	accept := ncip.AcceptItem{
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	_, err = ncipClient.AcceptItem(accept)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing AcceptItemResponse")

	delete := ncip.DeleteItem{}
	_, err = ncipClient.DeleteItem(delete)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing DeleteItemResponse")

	request := ncip.RequestItem{}
	_, err = ncipClient.RequestItem(request)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing RequestItemResponse")

	cancelRequest := ncip.CancelRequestItem{}
	_, err = ncipClient.CancelRequestItem(cancelRequest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CancelRequestItemResponse")

	checkInItem := ncip.CheckInItem{}
	_, err = ncipClient.CheckInItem(checkInItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CheckInItemResponse")

	checkOutItem := ncip.CheckOutItem{}
	_, err = ncipClient.CheckOutItem(checkOutItem)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NCIP response: missing CheckOutItemResponse")

	createUserFiscalTransaction := ncip.CreateUserFiscalTransaction{}
	_, err = ncipClient.CreateUserFiscalTransaction(createUserFiscalTransaction)
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

	ncipData := make(map[string]any)
	ncipData["lookup_user_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = server.URL

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "NCIP message processing failed: Some Problem: Details about the problem", err.Error())
}

func TestAcceptItemOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["accept_item_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	accept := ncip.AcceptItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	res, err := ncipClient.AcceptItem(accept)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestAcceptItemInvalidUser(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["accept_item_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	accept := ncip.AcceptItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "foo",
		},
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	_, err := ncipClient.AcceptItem(accept)
	assert.Error(t, err)
	assert.Equal(t, "NCIP accept item failed: Unknown User: foo", err.Error())
}

func TestAcceptItemModeManual(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["accept_item_mode"] = "manual"

	lookup := ncip.AcceptItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		RequestId: ncip.RequestId{
			RequestIdentifierValue: "validrequest",
		},
	}
	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	res, err := ncipClient.AcceptItem(lookup)
	assert.NoError(t, err)
	assert.Nil(t, res)
}

func TestAcceptItemMissingNcipInfo(t *testing.T) {
	ncipClient := NewNcipClient(http.DefaultClient, nil)
	accept := ncip.AcceptItem{}
	_, err := ncipClient.AcceptItem(accept)
	assert.Error(t, err)
	assert.Equal(t, "missing accept_item_mode in NCIP configuration", err.Error())
}

func TestDeleteItemOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	delete := ncip.DeleteItem{}
	res, err := ncipClient.DeleteItem(delete)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestDeleteItemMissingNcipInfo(t *testing.T) {
	ncipClient := NewNcipClient(http.DefaultClient, nil)
	delete := ncip.DeleteItem{}
	_, err := ncipClient.DeleteItem(delete)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in configuration", err.Error())
}

func TestRequestItemOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["request_item_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
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
	_, err := ncipClient.RequestItem(request)
	assert.NoError(t, err)
}

func TestRequestItemModeManual(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["request_item_mode"] = "manual"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.RequestItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	res, err := ncipClient.RequestItem(lookup)
	assert.NoError(t, err)
	assert.Nil(t, res)
}

func TestCancelRequestItemOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
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
	res, err := ncipClient.CancelRequestItem(request)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestCheckInItemOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	request := ncip.CheckInItem{
		ItemId: ncip.ItemId{
			ItemIdentifierValue: "item-001",
		},
	}
	res, err := ncipClient.CheckInItem(request)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestCheckInItemMissingNcipInfo(t *testing.T) {
	ncipClient := NewNcipClient(http.DefaultClient, nil)
	request := ncip.CheckInItem{}
	_, err := ncipClient.CheckInItem(request)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in configuration", err.Error())
}

func TestCheckOutItemOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	request := ncip.CheckOutItem{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
		ItemId: ncip.ItemId{
			ItemIdentifierValue: "item-001",
		},
	}
	_, err := ncipClient.CheckOutItem(request)
	assert.NoError(t, err)
}

func TestCheckOutItemMissingNcipInfo(t *testing.T) {
	ncipClient := NewNcipClient(http.DefaultClient, nil)
	request := ncip.CheckOutItem{}
	_, err := ncipClient.CheckOutItem(request)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in configuration", err.Error())
}

func TestCreateUserFiscalTransactionOK(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["create_user_fiscal_transaction_mode"] = "auto"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["from_agency_authentication"] = "pass"
	ncipData["to_agency"] = "ILL-MOCK"
	ncipData["address"] = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
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
	res, err := ncipClient.CreateUserFiscalTransaction(lookup)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestCreateUserFiscalTransactionBadMode(t *testing.T) {
	ncipData := make(map[string]any)
	ncipData["create_user_fiscal_transaction_mode"] = "foo"
	ncipData["from_agency"] = "ILL-MOCK"
	ncipData["to_agency"] = "ILL-MOCK"

	ncipClient := NewNcipClient(http.DefaultClient, ncipData)
	lookup := ncip.CreateUserFiscalTransaction{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.CreateUserFiscalTransaction(lookup)
	assert.Error(t, err)
	assert.Equal(t, "unknown value for create_user_fiscal_transaction_mode: foo", err.Error())
}

func TestCreateUserFiscalTransactionMissingNcipInfo(t *testing.T) {
	ncipClient := NewNcipClient(http.DefaultClient, nil)
	request := ncip.CreateUserFiscalTransaction{}
	_, err := ncipClient.CreateUserFiscalTransaction(request)
	assert.Error(t, err)
	assert.Equal(t, "missing create_user_fiscal_transaction_mode in NCIP configuration", err.Error())
}
