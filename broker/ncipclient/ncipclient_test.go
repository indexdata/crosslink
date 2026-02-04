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

func createTestClient() NcipClient {
	return NewNcipClient(http.DefaultClient,
		"http://localhost:"+os.Getenv("HTTP_PORT")+"/ncip",
		"ILL-MOCK",
		"ILL-MOCK",
		"pass").(*NcipClientImpl)
}

func TestPrepareHeaderValues(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK2"
	ncipClient.toAgency = "ILL-MOCK1"
	ncipClient.fromAgencyAuthentication = "pass"

	header := ncipClient.prepareHeader(nil)
	assert.Equal(t, "ILL-MOCK1", header.ToAgencyId.AgencyId.Text)
	assert.Equal(t, "ILL-MOCK2", header.FromAgencyId.AgencyId.Text)
	assert.Equal(t, "pass", header.FromAgencyAuthentication)
}

func TestLookupUserAutoOK(t *testing.T) {
	ncipClient := createTestClient()
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
	ncipClient := createTestClient()
	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "foo",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "NCIP user lookup failed: Unknown User: foo", err.Error())
}

func TestLookupUserMissingAddress(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.toAgency = "ILL-MOCK"

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

func TestBadNcipMessageResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("<myType><msg>OK</msg></myType>"))
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = server.URL

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

	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = server.URL
	var logOutgoing []byte
	var logIncoming []byte
	var logError error

	ncipClient.logFunc = func(outgoing []byte, incoming []byte, err error) {
		logOutgoing = outgoing
		logIncoming = incoming
		logError = err
	}

	lookup := ncip.LookupUser{
		UserId: &ncip.UserId{
			UserIdentifierValue: "validuser",
		},
	}
	_, err := ncipClient.LookupUser(lookup)
	assert.Error(t, err)
	assert.Equal(t, "invalid NCIP response: missing LookupUserResponse", err.Error())
	assert.NotNil(t, logOutgoing)
	assert.NotNil(t, logIncoming)
	assert.Nil(t, logError)

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

	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = server.URL

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
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
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
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
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

func TestDeleteItemOK(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

	delete := ncip.DeleteItem{}
	res, err := ncipClient.DeleteItem(delete)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestRequestItemOK(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

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

func TestCancelRequestItemOK(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

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
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"
	request := ncip.CheckInItem{
		ItemId: ncip.ItemId{
			ItemIdentifierValue: "item-001",
		},
	}
	res, err := ncipClient.CheckInItem(request)
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

func TestCheckOutItemOK(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

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

func TestCreateUserFiscalTransactionOK(t *testing.T) {
	ncipClient := NcipClientImpl{}
	ncipClient.client = http.DefaultClient
	ncipClient.fromAgency = "ILL-MOCK"
	ncipClient.fromAgencyAuthentication = "pass"
	ncipClient.toAgency = "ILL-MOCK"
	ncipClient.address = "http://localhost:" + os.Getenv("HTTP_PORT") + "/ncip"

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
