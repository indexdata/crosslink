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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
)

func TestParseConfig(t *testing.T) {
	os.Setenv("HTTP_PORT", "8082")
	os.Setenv("PEER_URL", "https://localhost:8082")
	os.Setenv("AGENCY_TYPE", "ABC")
	os.Setenv("SUPPLYING_AGENCY_ID", "S1")
	os.Setenv("REQUESTING_AGENCY_ID", "R1")
	var app MockApp
	app.parseConfig()
	assert.Equal(t, "8082", app.httpPort)
	assert.Equal(t, "ABC", app.agencyType)
	assert.Equal(t, "S1", app.requester.supplyingAgencyId)
	assert.Equal(t, "R1", app.requester.requestingAgencyId)
	assert.Equal(t, "https://localhost:8082", app.peerUrl)
}

// getFreePort asks the kernel for a free open port that is ready to use.
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	// release for now so it can be bound by the actual server
	// a more robust solution would be to bind the server to the port and close it here
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// getFreePortTest returns a free port as a string for testing.
func getFreePortTest(t *testing.T) string {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get a free port: %v", err)
	}
	return strconv.Itoa(port)
}

func TestAppShutdown(t *testing.T) {
	var app MockApp
	app.Shutdown() // no server running
}

func TestService(t *testing.T) {
	var app MockApp
	dynPort := getFreePortTest(t)
	app.httpPort = dynPort
	app.peerUrl = "http://localhost:" + dynPort
	isoUrl := "http://localhost:" + dynPort + "/iso18626"
	go func() {
		err := app.Run()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("app.Run error %s", err.Error())
		}
	}()
	time.Sleep(5 * time.Millisecond) // wait for app to serve

	t.Run("Bad method", func(t *testing.T) {
		resp, err := http.Get(isoUrl)
		assert.Nil(t, err)
		assert.Equal(t, 405, resp.StatusCode)
	})
	t.Run("Bad content type", func(t *testing.T) {
		resp, err := http.Post(isoUrl, "text/plain", strings.NewReader("hello"))
		assert.Nil(t, err)
		assert.Equal(t, 415, resp.StatusCode)
	})

	t.Run("Bad XML", func(t *testing.T) {
		resp, err := http.Post(isoUrl, "text/xml", strings.NewReader("<badxml"))
		assert.Nil(t, err)
		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("Invalid message", func(t *testing.T) {
		var msg = &iso18626.Iso18626MessageNS{}
		msg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{}
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("Empty RequestingAgencyRequestId", func(t *testing.T) {
		msg := createRequest()
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(isoUrl, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 200, resp.StatusCode)
		// TOOD: check response body
		defer resp.Body.Close()
		buf, err = io.ReadAll(resp.Body)
		assert.Nil(t, err)
		var response iso18626.ISO18626Message
		err = xml.Unmarshal(buf, &response)
		assert.Nil(t, err)
		assert.NotNil(t, response.RequestConfirmation)
		assert.Equal(t, "Requesting agency request id cannot be empty", response.RequestConfirmation.ErrorData.ErrorValue)
	})

	t.Run("Reuse RequestingAgencyRequestId", func(t *testing.T) {
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
		assert.Equal(t, iso18626.TypeMessageStatusOK, response.RequestConfirmation.ConfirmationHeader.MessageStatus)

		msg = createRequest()
		msg.Request.Header.RequestingAgencyRequestId = "1"
		msg.Request.Header.SupplyingAgencyId.AgencyIdValue = "S2"
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

	t.Run("Non existing RequestingAgencyRequestId", func(t *testing.T) {
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
		assert.Equal(t, "Non existing RequestingAgencyRequestId", response.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
	})

	t.Run("Patron request scenarios", func(t *testing.T) {
		for _, scenario := range []string{"WILLSUPPLY_LOANED", "WILLSUPPLY_UNFILLED", "UNFILLED", "LOANED"} {
			msg := createPatronRequest()
			msg.Request.BibliographicInfo.SupplierUniqueRecordId = scenario
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
			assert.Nil(t, response.RequestConfirmation.ErrorData)
		}
		time.Sleep(500 * time.Millisecond)
	})

	t.Run("Patron request, connection refused / bad peer URL", func(t *testing.T) {
		// connect to port with no listening server
		port, err := getFreePort()
		assert.Nil(t, err)
		// when we can set peer URL per request, this will be easier
		app.peerUrl = "http://localhost:" + strconv.Itoa(port)
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

	t.Run("Patron request, no request confirmation", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/xml")
			w.WriteHeader(http.StatusOK)
			output, _ := xml.Marshal(&iso18626.Iso18626MessageNS{})
			_, err := w.Write(output)
			assert.Nil(t, err)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
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
		assert.Equal(t, "Did not receive requestConfirmation from supplier", response.RequestConfirmation.ErrorData.ErrorValue)
	})

	t.Run("writeResponse null", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeResponse(nil, w)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		msg := createPatronRequest()
		msg.Request.BibliographicInfo.SupplierUniqueRecordId = "WILLSUPPLY_LOANED"
		buf := utils.Must(xml.Marshal(msg))
		resp, err := http.Post(server.URL, "text/xml", bytes.NewReader(buf))
		assert.Nil(t, err)
		assert.Equal(t, 500, resp.StatusCode)
	})

	t.Run("sendRequestingAgency no key", func(t *testing.T) {
		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		app.sendRequestingAgencyMessage(header)
	})

	t.Run("sendRequestingAgency internal error", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()

		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"

		requesterInfo := &requesterInfo{action: iso18626.TypeActionCancel}
		app.requester.store(header, requesterInfo)
		app.sendRequestingAgencyMessage(header)
	})

	t.Run("sendRequestingAgency unexpected ISO18626 message", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var resmsg = &iso18626.Iso18626MessageNS{}
			writeResponse(resmsg, w)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()

		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		requesterInfo := &requesterInfo{action: iso18626.TypeActionCancel}
		app.requester.store(header, requesterInfo)
		app.sendRequestingAgencyMessage(header)
	})

	t.Run("sendRequestingAgency action mismatch", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var resmsg = &iso18626.Iso18626MessageNS{}
			resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{}
			act := iso18626.TypeActionReceived
			resmsg.RequestingAgencyMessageConfirmation.Action = &act
			writeResponse(resmsg, w)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()

		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		requesterInfo := &requesterInfo{action: iso18626.TypeActionCancel}
		app.requester.store(header, requesterInfo)
		app.sendRequestingAgencyMessage(header)
	})

	t.Run("sendSupplyingAgencyMessage no key", func(t *testing.T) {
		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"
		app.sendSupplyingAgencyMessage(header)
	})

	t.Run("sendSuppluingAgency internal error", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()

		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"

		supplierInfo := &supplierInfo{index: 0, status: []iso18626.TypeStatus{iso18626.TypeStatusWillSupply}}
		app.supplier.store(header, supplierInfo)
		app.sendSupplyingAgencyMessage(header)
	})

	t.Run("sendSupplyingAgency unexpected ISO18626 message", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var resmsg = &iso18626.Iso18626MessageNS{}
			writeResponse(resmsg, w)
		})
		server := httptest.NewServer(handler)
		defer server.Close()

		app.peerUrl = server.URL
		defer func() { app.peerUrl = "http://localhost:" + dynPort }()

		header := &iso18626.Header{}
		header.RequestingAgencyRequestId = uuid.NewString()
		header.SupplyingAgencyId.AgencyIdValue = "S1"
		header.RequestingAgencyId.AgencyIdValue = "R1"

		supplierInfo := &supplierInfo{index: 0, status: []iso18626.TypeStatus{iso18626.TypeStatusWillSupply}}
		app.supplier.store(header, supplierInfo)
		app.sendSupplyingAgencyMessage(header)
	})

	err := app.Shutdown()
	assert.Nil(t, err)
}
