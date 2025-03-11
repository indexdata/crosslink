package app

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/illmock/flows"
	"github.com/indexdata/crosslink/illmock/testutil"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
)

func createPatronRequest() *iso18626.Iso18626MessageNS {
	var msg = createRequest()
	msg.Request = &iso18626.Request{}
	msg.Request.ServiceInfo = &iso18626.ServiceInfo{}
	si := iso18626.TypeRequestTypeNew
	msg.Request.ServiceInfo.RequestType = &si
	msg.Request.ServiceInfo.RequestSubType = []iso18626.TypeRequestSubType{iso18626.TypeRequestSubTypePatronRequest}
	return msg
}

func TestParseEnv(t *testing.T) {
	os.Setenv("HTTP_PORT", "8082")
	os.Setenv("PEER_URL", "https://localhost:8082")
	os.Setenv("AGENCY_TYPE", "ABC")
	os.Setenv("SUPPLYING_AGENCY_ID", "S1")
	os.Setenv("REQUESTING_AGENCY_ID", "R1")
	os.Setenv("SUPPLY_DURATION", "100ms")
	var app MockApp
	err := app.parseEnv()
	assert.Nil(t, err)
	assert.Equal(t, "8082", app.httpPort)
	assert.Equal(t, "ABC", app.agencyType)
	assert.Equal(t, "S1", app.requester.supplyingAgencyId)
	assert.Equal(t, "R1", app.requester.requestingAgencyId)
	assert.Equal(t, "https://localhost:8082", app.peerUrl)
	assert.Equal(t, 100*time.Millisecond, app.supplyDuration)
	os.Unsetenv("HTTP_PORT")
	os.Unsetenv("PEER_URL")
	os.Unsetenv("AGENCY_TYPE")
	os.Unsetenv("SUPPLYING_AGENCY_ID")
	os.Unsetenv("REQUESTING_AGENCY_ID")
	os.Unsetenv("SUPPLY_DURATION")
}

func TestAppBadSupplyDuration(t *testing.T) {
	os.Setenv("SUPPLY_DURATION", "x")
	var app MockApp
	err := app.Run()
	os.Unsetenv("SUPPLY_DURATION")
	assert.ErrorContains(t, err, "invalid SUPPLY_DURATION: time: ")
}

func TestGetSupplyDuration(t *testing.T) {
	dur, err := getSupplyDuration("3ms")
	assert.Nil(t, err)
	assert.Equal(t, 3*time.Millisecond, dur)

	_, err = getSupplyDuration("x")
	assert.ErrorContains(t, err, "invalid SUPPLY_DURATION: time: ")

	_, err = getSupplyDuration("-3ms")
	assert.ErrorContains(t, err, "SUPPLY_DURATION can not be negative")
}

func TestAppShutdown(t *testing.T) {
	var app MockApp
	err := app.Shutdown() // no server running
	assert.Nil(t, err)
}

func TestSendReceiveUrlEmpty(t *testing.T) {
	var app MockApp
	_, err := app.sendReceive("", nil, "supplier", nil)
	assert.ErrorContains(t, err, "url cannot be empty")
}

func TestSendReceiveUnmarshalFailed(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		output := []byte("<")
		_, err := w.Write(output)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	app.peerUrl = server.URL
	msg := iso18626.NewIso18626MessageNS()
	msg.Request = &iso18626.Request{Header: iso18626.Header{
		SupplyingAgencyId:         iso18626.TypeAgencyId{AgencyIdValue: "S1"},
		RequestingAgencyId:        iso18626.TypeAgencyId{AgencyIdValue: "R1"},
		RequestingAgencyRequestId: uuid.NewString(),
	}}
	_, err := app.sendReceive(app.peerUrl, msg, "supplier", &msg.Request.Header)
	assert.ErrorContains(t, err, "unexpected EOF")
}

func TestLogIncomingReq(t *testing.T) {
	header := &iso18626.Header{}
	var app MockApp
	app.logIncomingReq("supplier", header, nil)
}

func TestLogOutgoingReq(t *testing.T) {
	header := &iso18626.Header{}
	var app MockApp
	app.logOutgoingReq("supplier", header, nil, "http://localhost")
}

func TestLogIncomingRes(t *testing.T) {
	header := &iso18626.Header{}
	var app MockApp
	app.logIncomingRes("supplier", header, nil, "http://localhost")
}

func TestLogOutgoingRes(t *testing.T) {
	header := &iso18626.Header{}
	var app MockApp
	app.logOutgoingRes("supplier", header, nil)
}

func TestLogOutgoingErr(t *testing.T) {
	header := &iso18626.Header{}
	var app MockApp
	app.logOutgoingErr("supplier", header, "http://localhost", 500, "service unavailable")
}

func TestWriteResponseNil(t *testing.T) {
	var app MockApp
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.writeIso18626Response(nil, w, "requester", nil)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	msg := createPatronRequest()
	buf := utils.Must(xml.Marshal(msg))
	resp, err := http.Post(server.URL, "text/xml", bytes.NewReader(buf))
	assert.Nil(t, err)
	assert.Equal(t, 500, resp.StatusCode)
	defer resp.Body.Close()
	buf, err = io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Contains(t, string(buf), "marshal failed")
}

func TestFlowsApiParseEnvFailed(t *testing.T) {
	os.Setenv("CLEAN_TIMEOUT", "0")
	var app MockApp
	err := app.Run()
	os.Unsetenv("CLEAN_TIMEOUT")
	assert.ErrorContains(t, err, "CLEAN_TIMEOUT must be greater than 0")
}

func runScenario(t *testing.T, isoUrl string, apiUrl string, msg *iso18626.Iso18626MessageNS,
	scenario string, expectedLen int) []flows.FlowMessage {

	requesterId := uuid.NewString()
	msg.Request.Header.RequestingAgencyId.AgencyIdValue = requesterId
	msg.Request.BibliographicInfo.SupplierUniqueRecordId = scenario
	buf, err := xml.Marshal(msg)
	assert.Nil(t, err)
	resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	defer resp.Body.Close()
	buf, err = io.ReadAll(resp.Body)
	assert.Nil(t, err)
	var response iso18626.ISO18626Message
	err = xml.Unmarshal(buf, &response)
	assert.Nil(t, err)
	assert.NotNil(t, response.RequestConfirmation)
	assert.Equal(t, iso18626.TypeMessageStatusOK, response.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Nil(t, response.RequestConfirmation.ErrorData)

	for tries := 0; tries < 5; tries++ {
		time.Sleep(400 * time.Millisecond)
		resp, err = http.Get(apiUrl + "?requester=" + requesterId + "&role=requester")
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var flowR flows.Flows
		err = xml.Unmarshal(buf, &flowR)
		assert.Nil(t, err)
		assert.Len(t, flowR.Flows, 1)
		assert.NotNil(t, flowR.Flows[0].Message[0].Message.Request.ServiceInfo)
		assert.Nil(t, flowR.Flows[0].Message[1].Message.Request.ServiceInfo)
		ret := flowR.Flows[0].Message
		assert.LessOrEqual(t, len(ret), expectedLen)
		if len(ret) == expectedLen {
			return ret
		}
	}
	assert.Fail(t, "Expected length not reached for scenario "+scenario)
	return nil
}

func TestService(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi() // FlowsApi.ParseEnv is not called
	dynPort := testutil.GetFreePortTest(t)
	app.httpPort = dynPort
	url := "http://localhost:" + dynPort
	app.peerUrl = url
	isoUrl := url + "/iso18626"
	apiUrl := url + "/api/flows"
	healthUrl := url + "/healthz"
	sruUrl := url + "/sru"
	app.agencyType = "ABC"
	app.tenantId = "T1"
	go func() {
		err := app.Run()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("app.Run error %s", err.Error())
		}
	}()
	time.Sleep(5 * time.Millisecond) // wait for app to serve

	t.Run("notfound", func(t *testing.T) {
		resp, err := http.Get(url + "/foo")
		assert.Nil(t, err)
		assert.Equal(t, 404, resp.StatusCode)
	})

	t.Run("health: ok", func(t *testing.T) {
		resp, err := http.Get(healthUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
	})

	t.Run("health: method", func(t *testing.T) {
		resp, err := http.Post(healthUrl, "text/plain", strings.NewReader("Hello"))
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})

	t.Run("flows handler: ok", func(t *testing.T) {
		resp, err := http.Get(apiUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Contains(t, string(buf), "<flows")
	})

	t.Run("flows handler: Bad method", func(t *testing.T) {
		resp, err := http.Post(apiUrl, "text/plain", strings.NewReader("hello"))
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})

	t.Run("sru handler: ok", func(t *testing.T) {
		resp, err := http.Get(sruUrl)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err := io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Contains(t, string(buf), "<explainResponse")
	})

	t.Run("iso18626 handler: Bad method", func(t *testing.T) {
		resp, err := http.Get(isoUrl)
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})
	t.Run("iso18626 handler: Bad content type", func(t *testing.T) {
		resp, err := http.Post(isoUrl, "text/plain", strings.NewReader("hello"))
		assert.Nil(t, err)
		assert.Equal(t, 415, resp.StatusCode)
	})

	t.Run("iso18626 handler: Read fail", func(t *testing.T) {
		conn, err := net.Dial("tcp", "localhost:"+dynPort)
		assert.Nil(t, err)
		defer conn.Close()
		// Bad chunked stream
		n, err := conn.Write([]byte("POST /iso18626 HTTP/1.1\r\nHost: localhost\r\nTransfer-Encoding: chunked\r\nContent-Type: text/xml\r\n\r\n2\r\n"))
		assert.Nil(t, err)
		assert.Greater(t, n, 20)
	})

	t.Run("iso18626 handler: Bad XML", func(t *testing.T) {
		resp, err := http.Post(isoUrl, "text/xml", strings.NewReader("<badxml"))
		assert.Nil(t, err)
		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("iso18626 handler: Invalid message", func(t *testing.T) {
		var msg = iso18626.NewIso18626MessageNS()
		msg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{}
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("request: Empty RequestingAgencyRequestId", func(t *testing.T) {
		msg := createRequest()
		msg.Request.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		msg.Request.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, "RequestingAgencyRequestId cannot be empty", response.RequestConfirmation.ErrorData.ErrorValue)
	})

	t.Run("request: Empty SupplyingAgencyId", func(t *testing.T) {
		requesterId := uuid.NewString()
		msg := createRequest()
		msg.Request.Header.RequestingAgencyRequestId = uuid.NewString()
		msg.Request.Header.RequestingAgencyId.AgencyIdValue = requesterId
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, "SupplyingAgencyId cannot be empty", response.RequestConfirmation.ErrorData.ErrorValue)

		resp, err = http.Get(apiUrl + "?requester=" + requesterId)
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		assert.Contains(t, string(buf), ">"+msg.Request.Header.RequestingAgencyRequestId+"<")
		assert.Contains(t, string(buf), "id=\""+msg.Request.Header.RequestingAgencyRequestId)
	})

	t.Run("request: Empty RequestingAgencyId", func(t *testing.T) {
		msg := createRequest()
		msg.Request.Header.RequestingAgencyRequestId = "1"
		msg.Request.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, "RequestingAgencyId cannot be empty", response.RequestConfirmation.ErrorData.ErrorValue)
	})

	t.Run("request: Reuse RequestingAgencyRequestId", func(t *testing.T) {
		msg := createRequest()
		msg.Request.Header.RequestingAgencyRequestId = "1"
		msg.Request.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		msg.Request.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.Equal(t, iso18626.TypeMessageStatusOK, response.RequestConfirmation.ConfirmationHeader.MessageStatus)

		msg = createRequest()
		msg.Request.Header.RequestingAgencyRequestId = "1"
		msg.Request.Header.SupplyingAgencyId.AgencyIdValue = "S2"
		msg.Request.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf = utils.Must(xml.Marshal(msg))
		resp2, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp2.StatusCode)
		defer resp2.Body.Close()
		buf, err = io.ReadAll(resp2.Body)
		assert.Nil(t, err)
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.Equal(t, iso18626.TypeMessageStatusOK, response.RequestConfirmation.ConfirmationHeader.MessageStatus)

		msg = createRequest()
		msg.Request.Header.RequestingAgencyRequestId = "1"
		msg.Request.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		msg.Request.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf = utils.Must(xml.Marshal(msg))
		resp3, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp3.StatusCode)
		defer resp3.Body.Close()
		buf, err = io.ReadAll(resp3.Body)
		assert.Nil(t, err)
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, response.RequestConfirmation.ErrorData.ErrorType)
		assert.Equal(t, "RequestingAgencyRequestId already exists", response.RequestConfirmation.ErrorData.ErrorValue)
	})

	t.Run("requestingAgencyMessage: Empty RequestingAgencyRequestId", func(t *testing.T) {
		msg := createRequestingAgencyMessage()
		msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		msg.RequestingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.Nil(t, response.RequestConfirmation)
		assert.NotNil(t, response.RequestingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
		assert.Equal(t, "RequestingAgencyRequestId cannot be empty", response.RequestingAgencyMessageConfirmation.ErrorData.ErrorValue)
	})

	t.Run("requestingAgencyMessage: Empty Supplying Agency Id value", func(t *testing.T) {
		msg := createRequestingAgencyMessage()
		msg.RequestingAgencyMessage.Header.RequestingAgencyRequestId = uuid.NewString()
		msg.RequestingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.Nil(t, response.RequestConfirmation)
		assert.NotNil(t, response.RequestingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
		assert.Equal(t, "SupplyingAgencyId cannot be empty", response.RequestingAgencyMessageConfirmation.ErrorData.ErrorValue)
	})

	t.Run("requestingAgencyMessage: Empty Requesting Agency Id value", func(t *testing.T) {
		msg := createRequestingAgencyMessage()
		msg.RequestingAgencyMessage.Header.RequestingAgencyRequestId = uuid.NewString()
		msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.Nil(t, response.RequestConfirmation)
		assert.NotNil(t, response.RequestingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
		assert.Equal(t, "RequestingAgencyId cannot be empty", response.RequestingAgencyMessageConfirmation.ErrorData.ErrorValue)
	})

	t.Run("supplying agency message: missing ids", func(t *testing.T) {
		msg := createSupplyingAgencyMessage()
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorType)
		assert.Equal(t, "RequestingAgencyRequestId cannot be empty", response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
	})

	t.Run("supplying agency message: Non existing RequestingAgencyRequestId", func(t *testing.T) {
		msg := createSupplyingAgencyMessage()
		msg.SupplyingAgencyMessage.Header.RequestingAgencyRequestId = uuid.NewString()
		msg.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue = "S1"
		msg.SupplyingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue = "R1"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorType)
		assert.Equal(t, "Non existing RequestingAgencyRequestId", response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
	})

	t.Run("Patron request ERROR", func(t *testing.T) {
		scenario := "ERROR"
		msg := createPatronRequest()
		msg.Request.BibliographicInfo.SupplierUniqueRecordId = scenario
		buf, err := xml.Marshal(msg)
		assert.Nil(t, err)
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestConfirmation.ConfirmationHeader.MessageStatus)
		assert.NotNil(t, response.RequestConfirmation.ErrorData)
		assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataValue, response.RequestConfirmation.ErrorData.ErrorType)
		assert.Contains(t, response.RequestConfirmation.ErrorData.ErrorValue, "ERROR")
	})

	t.Run("Patron request HTTP-ERROR", func(t *testing.T) {
		for _, status := range []string{"400", "500"} {
			scenario := "HTTP-ERROR-" + status
			msg := createPatronRequest()
			msg.Request.BibliographicInfo.SupplierUniqueRecordId = scenario
			buf, err := xml.Marshal(msg)
			assert.Nil(t, err)
			resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
			assert.Nil(t, err)
			assert.Equal(t, 200, resp.StatusCode)
			defer resp.Body.Close()
			buf, err = io.ReadAll(resp.Body)
			assert.Nil(t, err)
			var response iso18626.ISO18626Message
			err = xml.Unmarshal(buf, &response)
			assert.Nil(t, err)
			assert.NotNil(t, response.RequestConfirmation)
			assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestConfirmation.ConfirmationHeader.MessageStatus)
			assert.NotNil(t, response.RequestConfirmation.ErrorData)
			assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataElement, response.RequestConfirmation.ErrorData.ErrorType)
			assert.Contains(t, response.RequestConfirmation.ErrorData.ErrorValue, status)
		}
	})

	t.Run("Patron request loaned", func(t *testing.T) {
		msg := createPatronRequest()
		ret := runScenario(t, isoUrl, apiUrl, msg, "LOANED", 12)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusLoanCompleted, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request willsupply loaned", func(t *testing.T) {
		msg := createPatronRequest()
		ret := runScenario(t, isoUrl, apiUrl, msg, "WILLSUPPLY_LOANED", 14)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusLoanCompleted, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request unfilled", func(t *testing.T) {
		msg := createPatronRequest()
		ret := runScenario(t, isoUrl, apiUrl, msg, "UNFILLED", 6)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusUnfilled, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request willsupply unfilled", func(t *testing.T) {
		msg := createPatronRequest()
		ret := runScenario(t, isoUrl, apiUrl, msg, "WILLSUPPLY_UNFILLED", 8)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusUnfilled, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request willsupply loaned overdue", func(t *testing.T) {
		msg := createPatronRequest()
		ret := runScenario(t, isoUrl, apiUrl, msg, "WILLSUPPLY_LOANED_OVERDUE", 16)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusLoanCompleted, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request loaned overdue renew", func(t *testing.T) {
		msg := createPatronRequest()
		msg.Request.ServiceInfo.Note = "#RENEW#"
		ret := runScenario(t, isoUrl, apiUrl, msg, "LOANED_OVERDUE", 18)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusLoanCompleted, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request cancel no", func(t *testing.T) {
		msg := createPatronRequest()
		msg.Request.ServiceInfo.Note = "#CANCEL#"
		ret := runScenario(t, isoUrl, apiUrl, msg, "LOANED", 16)
		assert.NotNil(t, ret[0].Message.Request.ServiceInfo)
		assert.Nil(t, ret[1].Message.Request.ServiceInfo)
		assert.NotNil(t, ret[7].Message.SupplyingAgencyMessage)
		assert.NotNil(t, ret[7].Message.SupplyingAgencyMessage.MessageInfo.AnswerYesNo)
		assert.Equal(t, iso18626.TypeYesNoN, *ret[7].Message.SupplyingAgencyMessage.MessageInfo.AnswerYesNo)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessage)
		assert.Equal(t, iso18626.TypeStatusLoanCompleted, m.SupplyingAgencyMessage.StatusInfo.Status)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
	})

	t.Run("Patron request cancel yes", func(t *testing.T) {
		msg := createPatronRequest()
		msg.Request.ServiceInfo.Note = "#CANCEL#"
		ret := runScenario(t, isoUrl, apiUrl, msg, "WILLSUPPLY_LOANED", 10)
		assert.NotNil(t, ret[0].Message.Request.ServiceInfo)
		assert.Nil(t, ret[1].Message.Request.ServiceInfo)
		assert.NotNil(t, ret[7].Message.SupplyingAgencyMessage)
		assert.NotNil(t, ret[7].Message.SupplyingAgencyMessage.MessageInfo.AnswerYesNo)
		assert.Equal(t, iso18626.TypeYesNoY, *ret[7].Message.SupplyingAgencyMessage.MessageInfo.AnswerYesNo)
		m := ret[len(ret)-2].Message
		assert.NotNil(t, m.SupplyingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, *m.SupplyingAgencyMessageConfirmation.ReasonForMessage)
		m = ret[len(ret)-1].Message
		assert.NotNil(t, m.RequestingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeActionCancel, *m.RequestingAgencyMessageConfirmation.Action)
	})

	t.Run("Patron request, connection refused / bad peer URL", func(t *testing.T) {
		// connect to port with no listening server
		port := testutil.GetFreePortTest(t)
		// when we can set peer URL per request, this will be easier
		app.peerUrl = "http://localhost:" + port
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()
		msg := createPatronRequest()
		msg.Request.BibliographicInfo.SupplierUniqueRecordId = "WILLSUPPLY_LOANED"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestConfirmation.ConfirmationHeader.MessageStatus)
		assert.NotNil(t, response.RequestConfirmation.ErrorData)
		assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataElement, response.RequestConfirmation.ErrorData.ErrorType)
		assert.Contains(t, response.RequestConfirmation.ErrorData.ErrorValue, "connection refused")
	})

	t.Run("Patron request, supplier URL", func(t *testing.T) {
		port := testutil.GetFreePortTest(t)
		app.peerUrl = "http://localhost:" + port // nothing listening here now!
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()
		msg := createPatronRequest()
		msg.Request.BibliographicInfo.SupplierUniqueRecordId = "WILLSUPPLY_LOANED"
		msg.Request.SupplierInfo = []iso18626.SupplierInfo{{SupplierDescription: "http://localhost:" + dynPort}}
		address := iso18626.Address{ElectronicAddress: &iso18626.ElectronicAddress{ElectronicAddressData: "http://localhost:" + dynPort}}
		msg.Request.RequestingAgencyInfo = &iso18626.RequestingAgencyInfo{Address: []iso18626.Address{address}}

		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusOK, response.RequestConfirmation.ConfirmationHeader.MessageStatus)
	})

	t.Run("Patron request, no request confirmation", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusOK)
			output, _ := xml.Marshal(iso18626.NewIso18626MessageNS())
			_, err := w.Write(output)
			assert.Nil(t, err)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()

		msg := createPatronRequest()
		// RequestingAgencyRequestId not provided, so it will be generated
		msg.Request.BibliographicInfo.SupplierUniqueRecordId = "WILLSUPPLY_LOANED"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		// Generated RequestingAgencyRequestId should be in response
		assert.NotEqual(t, "", response.RequestConfirmation.ConfirmationHeader.RequestingAgencyRequestId)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, response.RequestConfirmation.ConfirmationHeader.MessageStatus)
		assert.NotNil(t, response.RequestConfirmation.ErrorData)
		assert.Equal(t, iso18626.TypeErrorTypeUnrecognisedDataElement, response.RequestConfirmation.ErrorData.ErrorType)
		assert.Equal(t, "Did not receive requestConfirmation from supplier", response.RequestConfirmation.ErrorData.ErrorValue)
	})

	t.Run("Request shipped return", func(t *testing.T) {
		msg := createRequestingAgencyMessage()
		header := &msg.RequestingAgencyMessage.Header

		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"
		msg.RequestingAgencyMessage.Action = iso18626.TypeActionShippedReturn

		responseMsg, err := app.sendReceive(app.peerUrl, msg, "requester", header)
		assert.Nil(t, err)
		assert.NotNil(t, responseMsg.RequestingAgencyMessageConfirmation)
		assert.Equal(t, iso18626.TypeMessageStatusOK, responseMsg.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
		assert.Nil(t, responseMsg.RequestingAgencyMessageConfirmation.ErrorData)
		assert.NotNil(t, responseMsg.RequestingAgencyMessageConfirmation.Action)
		assert.Equal(t, iso18626.TypeActionShippedReturn, *responseMsg.RequestingAgencyMessageConfirmation.Action)
	})

	t.Run("tenant ID set", func(t *testing.T) {
		assert.Equal(t, "T1", app.tenantId)
		assert.Equal(t, "T1", app.client.Headers.Get("X-Okapi-Tenant"))
	})

	err := app.Shutdown()
	assert.Nil(t, err)
}

func TestSendRequestingAgencyNoKey(t *testing.T) {
	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	var app MockApp
	app.sendRequestingAgencyMessage(header, iso18626.TypeActionReceived)
}

func TestSendRequestingAgencyInternalError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var app MockApp
	app.peerUrl = server.URL
	app.flowsApi = flows.CreateFlowsApi()

	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"

	requesterInfo := &requesterInfo{supplierUrl: server.URL}
	app.requester.store(header, requesterInfo)
	app.sendRequestingAgencyMessage(header, iso18626.TypeActionCancel)
}

func TestSendRequestingAgencyUnexpectedISO18626Message(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resmsg = iso18626.NewIso18626MessageNS()
		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"
		app.writeIso18626Response(resmsg, w, "supplier", header)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	app.peerUrl = server.URL

	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	requesterInfo := &requesterInfo{supplierUrl: server.URL}
	app.requester.store(header, requesterInfo)
	app.sendRequestingAgencyMessage(header, iso18626.TypeActionCancel)
}

func TestSendRequestingAgencyActionMismatch(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resmsg = iso18626.NewIso18626MessageNS()
		resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{}
		act := iso18626.TypeActionReceived
		resmsg.RequestingAgencyMessageConfirmation.Action = &act
		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"
		app.writeIso18626Response(resmsg, w, "supplier", header)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	app.peerUrl = server.URL

	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	requesterInfo := &requesterInfo{supplierUrl: server.URL}
	app.requester.store(header, requesterInfo)
	app.sendRequestingAgencyMessage(header, iso18626.TypeActionCancel)
}

func TestSendRequestingAgencyActionNil(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var resmsg = iso18626.NewIso18626MessageNS()
		resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{}
		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"
		app.writeIso18626Response(resmsg, w, "supplier", header)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	app.peerUrl = server.URL

	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	requesterInfo := &requesterInfo{supplierUrl: server.URL}
	app.requester.store(header, requesterInfo)
	app.sendRequestingAgencyMessage(header, iso18626.TypeActionCancel)
}

func TestSendSupplyingAgencyMessageNoKey(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi()
	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	header.RequestingAgencyId.AgencyIdValue = "R1"
	app.sendSupplyingAgencyLater(header, []iso18626.TypeStatus{iso18626.TypeStatusLoaned})
}

func TestSendSuppluingAgencyInternalError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var app MockApp
	app.peerUrl = server.URL
	app.flowsApi = flows.CreateFlowsApi()

	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	header.RequestingAgencyId.AgencyIdValue = "R1"

	supplierInfo := &supplierInfo{requesterUrl: server.URL}
	app.supplier.store(header, supplierInfo)
	app.sendSupplyingAgencyLater(header, []iso18626.TypeStatus{iso18626.TypeStatusLoaned})
}

func TestSendSupplyingAgencyUnexpectedISO18626message(t *testing.T) {
	var app MockApp
	app.flowsApi = flows.CreateFlowsApi()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"
		resmsg := createRequestResponse(header, iso18626.TypeMessageStatusOK, nil, nil)
		app.writeIso18626Response(resmsg, w, "requester", header)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	app.peerUrl = server.URL
	app.flowsApi = flows.CreateFlowsApi()

	header := &iso18626.Header{}
	header.RequestingAgencyRequestId = uuid.NewString()
	header.SupplyingAgencyId.AgencyIdValue = "S1"
	header.RequestingAgencyId.AgencyIdValue = "R1"

	supplierInfo := &supplierInfo{requesterUrl: server.URL}
	app.supplier.store(header, supplierInfo)
	app.sendSupplyingAgencyLater(header, []iso18626.TypeStatus{iso18626.TypeStatusLoaned})
}
