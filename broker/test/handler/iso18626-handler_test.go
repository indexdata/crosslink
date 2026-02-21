package handler

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/vcs"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	mocks "github.com/indexdata/crosslink/broker/test/mocks"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var mockIllRepoSuccess = new(mocks.MockIllRepositorySuccess)
var mockEventRepoSuccess = new(mocks.MockEventRepositorySuccess)
var eventBussSuccess = events.NewPostgresEventBus(mockEventRepoSuccess, "mock")
var mockIllRepoError = new(mocks.MockIllRepositoryError)
var mockEventRepoError = new(mocks.MockEventRepositoryError)
var eventBussError = events.NewPostgresEventBus(mockEventRepoError, "mock")
var dirAdapter = new(adapter.MockDirectoryLookupAdapter)
var illRepo ill_db.IllRepo

func TestMain(m *testing.M) {
	ctx := context.Background()
	app.DB_PROVISION = true
	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	mockPort := utils.Must(test.GetFreePort())
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	test.Expect(os.Setenv("PEER_URL", "http://localhost:"+strconv.Itoa(app.HTTP_PORT)), "failed to set peer URL")

	apptest.StartMockApp(mockPort)
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	adapter.MOCK_CLIENT_URL = "http://localhost:" + strconv.Itoa(mockPort) + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, illRepo, _, _ = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestIso18626PostHandlerSuccess(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	msgOk := "<messageStatus>OK</messageStatus>"
	assert.Contains(t, rr.Body.String(), msgOk)
}

func TestIso18626PostHandlerWrongMethod(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("GET", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	assert.Equal(t, "only POST allowed\n", rr.Body.String())
}

func TestIso18626PostHandlerWrongContentType(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusUnsupportedMediaType, rr.Code)
	assert.Equal(t, "only application/xml or text/xml accepted\n", rr.Body.String())
}

func TestIso18626PostHandlerInvalidBody(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("Invalid")))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "failure parsing request\n", rr.Body.String())
}

func TestIso18626PostHandlerTooLarge(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, 1)(rr, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
	assert.Equal(t, "request body too large\n", rr.Body.String())
}

func TestIso18626PostHandlerInvalid(t *testing.T) {
	data, _ := os.ReadFile("../testdata/msg-invalid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "invalid ISO18626 message\n", rr.Body.String())
}

func norm(in string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(in, "\n", ""), "\t", ""), " ", "")
}

func TestIso18626PostHandlerFailToLocateRequesterSymbol(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoError, eventBussError, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	errStatus := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), errStatus)
	errData := `<errorData>
 		<errorType>UnrecognisedDataValue</errorType>
 		<errorValue>requestingAgencyId: requesting agency not found</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

func TestIso18626PostHandlerFailToSave(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	var mockRepo = &MockRepositoryOnlyPeersOK{}
	handler.Iso18626PostHandler(mockRepo, eventBussError, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "failed to process request\n", rr.Body.String())
}

func TestIso18626PostHandlerMissingRequestingId(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-no-reqid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	errStatus := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), errStatus)
	errData := `<errorData>
 		<errorType>UnrecognisedDataValue</errorType>
 		<errorValue>requestingAgencyRequestId: cannot be empty</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

func TestIso18626PostRequestExists(t *testing.T) {
	data, _ := os.ReadFile("../testdata/request-willsupply-unfilled-willsupply-loaned.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(&MockRepositoryReqExists{}, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	msgOk := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), msgOk)
	errData := `<errorData>
		<errorType>UnrecognisedDataValue</errorType>
		<errorValue>requestingAgencyRequestId: request with a given ID already exists</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

// largely copied from illmock/app/requester.go
func createConfirmationHeader(inHeader *iso18626.Header, messageStatus iso18626.TypeMessageStatus) *iso18626.ConfirmationHeader {
	var header = &iso18626.ConfirmationHeader{}
	header.RequestingAgencyId = &iso18626.TypeAgencyId{}
	header.RequestingAgencyId.AgencyIdType = inHeader.RequestingAgencyId.AgencyIdType
	header.RequestingAgencyId.AgencyIdValue = inHeader.RequestingAgencyId.AgencyIdValue
	header.TimestampReceived = inHeader.Timestamp
	header.RequestingAgencyRequestId = inHeader.RequestingAgencyRequestId

	if len(inHeader.SupplyingAgencyId.AgencyIdValue) != 0 {
		header.SupplyingAgencyId = &iso18626.TypeAgencyId{}
		header.SupplyingAgencyId.AgencyIdType = inHeader.SupplyingAgencyId.AgencyIdType
		header.SupplyingAgencyId.AgencyIdValue = inHeader.SupplyingAgencyId.AgencyIdValue
	}

	header.Timestamp = utils.XSDDateTime{Time: time.Now()}
	header.MessageStatus = messageStatus
	return header
}

// largely copied from illmock/app/requester.go
func createErrorData(errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ErrorData {
	if errorMessage != nil {
		var errorData = iso18626.ErrorData{
			ErrorType:  *errorType,
			ErrorValue: *errorMessage,
		}
		return &errorData
	}
	return nil
}

// largely copied from illmock/app/requester.go
func createSupplyingAgencyResponse(supplyingAgencyMessage *iso18626.SupplyingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = iso18626.NewIso18626MessageNS()
	header := createConfirmationHeader(&supplyingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func TestIso18626PostSupplyingMessageConfirm(t *testing.T) {
	var httpStatus *int
	var messageStatus *iso18626.TypeMessageStatus
	// setup a fake requester server to respond to the supplying message
	// could use the mock instead if we could inject a fake requester ID ("reqid")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(*httpStatus)
		if *httpStatus != http.StatusOK {
			http.Error(w, http.StatusText(*httpStatus), *httpStatus)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var illMessage iso18626.Iso18626MessageNS
		err = xml.Unmarshal(byteReq, &illMessage)
		assert.NoError(t, err)
		supplyingAgencyMessage := illMessage.SupplyingAgencyMessage
		assert.NotNil(t, supplyingAgencyMessage)

		resmsg := createSupplyingAgencyResponse(supplyingAgencyMessage, *messageStatus, nil, nil)

		output, err := xml.Marshal(resmsg)
		assert.NoError(t, err)
		_, err = w.Write(output)
		assert.NoError(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	illId := uuid.NewString()
	requester := apptest.CreatePeer(t, illRepo, "ISIL:SUP_A", server.URL)
	_, err := illRepo.SaveIllTransaction(appCtx, ill_db.SaveIllTransactionParams{
		ID:                 illId,
		Timestamp:          test.GetNow(),
		RequesterRequestID: apptest.CreatePgText("6ad1ff2e-bab2-4978-b064-656c0e67ebd6"),
		RequesterSymbol:    apptest.CreatePgText("ISIL:SUP_A"),
		RequesterID:        apptest.CreatePgText(requester.ID),
	})
	assert.NoError(t, err)
	supplier := apptest.CreatePeer(t, illRepo, "ISIL:SUP_B", adapter.MOCK_CLIENT_URL)
	locSup := apptest.CreateLocatedSupplier(t, illRepo, illId, supplier.ID, "ISIL:SUP_B", "WillSupply")
	supplier.Url = adapter.MOCK_CLIENT_URL
	supplier, err = illRepo.SavePeer(appCtx, ill_db.SavePeerParams(supplier))
	assert.NoError(t, err)
	_, err = illRepo.SaveLocatedSupplier(appCtx, ill_db.SaveLocatedSupplierParams(locSup))
	assert.NoError(t, err)

	data, err := os.ReadFile("../testdata/supmsg-ok.xml")
	assert.NoError(t, err)
	url := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	for _, tt := range []struct {
		name          string
		httpStatus    int
		messageStatus iso18626.TypeMessageStatus
		contains      string
	}{
		{
			name:          "ResponseSuccessful",
			httpStatus:    200,
			messageStatus: iso18626.TypeMessageStatusOK,
			contains:      "<messageStatus>OK</messageStatus>",
		},
		{
			name:          "Response500",
			httpStatus:    500,
			messageStatus: iso18626.TypeMessageStatusOK,
			contains:      "Internal Server Error",
		},
		{
			name:          "Response500WithErrorStatus",
			httpStatus:    200,
			messageStatus: iso18626.TypeMessageStatusERROR,
			contains:      "<messageStatus>ERROR</messageStatus>",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			httpStatus = &tt.httpStatus
			messageStatus = &tt.messageStatus

			req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
			req.Header.Add("Content-Type", "application/xml")
			client := &http.Client{}
			res, err := client.Do(req)
			assert.NoError(t, err)
			assert.Equal(t, tt.httpStatus, res.StatusCode)
			body, err := io.ReadAll(res.Body)
			assert.NoError(t, err)
			assert.Equal(t, vcs.GetSignature(), res.Header.Get("Server"))
			assert.Contains(t, string(body), tt.contains)
		})
	}
}

func TestIso18626PostSupplyingMessageIncorrectSupplier(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(new(MockRepositoryOtherSupplier), eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	msgError := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), msgError)
	errorValue := "<errorValue>supplyingAgencyId: supplying agency not found or invalid</errorValue>"
	assert.Contains(t, rr.Body.String(), errorValue)
}

func TestIso18626PostSupplyingMessageErrorFindingSupplier(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(new(MockRepositorySupplierError), eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestIso18626PostSupplyingMessageDBFailure(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoError, eventBussError, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "failed to process request\n", rr.Body.String())
}

func TestIso18626PostSupplyingMessageNoReqId(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-no-reqid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	msgOk := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), msgOk)
	errData := `<errorData>
		<errorType>UnrecognisedDataValue</errorType>
		<errorValue>requestingAgencyRequestId: cannot be empty</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

func TestIso18626PostSupplyingMessageReqNotFound(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(&MockRepositoryReqNotFound{}, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	errStatus := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), errStatus)
	errData := `<errorData>
 		<errorType>UnrecognisedDataValue</errorType>
 		<errorValue>requestingAgencyRequestId: request with a given ID not found</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

func TestIso18626PostRequestingMessage(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		contains       string
		urlEnding      string
		supplierSymbol string
		skipped        bool
		useMock        bool
	}{
		{
			name:      "ResponseSuccessful",
			status:    200,
			contains:  "<messageStatus>OK</messageStatus>",
			urlEnding: "",
			useMock:   true,
		},
		{
			name:      "Response400",
			status:    400,
			contains:  "Bad request",
			urlEnding: "/error400",
			useMock:   true,
		},
		{
			name:      "Response500",
			status:    500,
			contains:  "Internal server error",
			urlEnding: "/error500",
			useMock:   true,
		},
		{
			name:      "ResponseBadlyFormedMessage",
			status:    200,
			contains:  "<errorType>BadlyFormedMessage</errorType>",
			urlEnding: "/notExists",
			useMock:   false,
		},
		{
			name:           "ResponseSupplierNotFoundOrInvalid-WrongSymbol",
			status:         200,
			contains:       "<errorValue>supplyingAgencyId: supplying agency not found or invalid</errorValue>",
			urlEnding:      "",
			supplierSymbol: "ISIL:SLNP_TWO_B",
			useMock:        true,
		},
		{
			name:           "ResponseSupplierNotFoundOrInvalid-Skipped",
			status:         200,
			contains:       "<errorValue>supplyingAgencyId: supplying agency not found or invalid</errorValue>",
			urlEnding:      "",
			supplierSymbol: "ISIL:SLNP_TWO_A",
			skipped:        true,
			useMock:        true,
		},
	}
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	data, _ := os.ReadFile("../testdata/reqmsg-notification.xml")
	illId := uuid.NewString()
	requester := apptest.CreatePeer(t, illRepo, "ISIL:SLNP_ONE", adapter.MOCK_CLIENT_URL)
	_, err := illRepo.SaveIllTransaction(appCtx, ill_db.SaveIllTransactionParams{
		ID:                 illId,
		Timestamp:          test.GetNow(),
		RequesterRequestID: apptest.CreatePgText("reqid"),
		RequesterSymbol:    apptest.CreatePgText("ISIL:SLNP_ONE"),
		RequesterID:        apptest.CreatePgText(requester.ID),
	})
	if err != nil {
		t.Errorf("failed to create ill transaction: %s", err)
	}
	supplier := apptest.CreatePeer(t, illRepo, "ISIL:SLNP_TWO_A", adapter.MOCK_CLIENT_URL)
	locSup := apptest.CreateLocatedSupplier(t, illRepo, illId, supplier.ID, "ISIL:SLNP_TWO_A", "selected")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.useMock {
				supplier.Url = adapter.MOCK_CLIENT_URL + tt.urlEnding
			} else {
				port, _ := test.GetFreePort()
				supplier.Url = "http:localhost:" + strconv.Itoa(port) + tt.urlEnding
			}
			supplier, err = illRepo.SavePeer(appCtx, ill_db.SavePeerParams(supplier))
			if err != nil {
				t.Errorf("failed to update supplier peer : %s", err)
			}
			if tt.supplierSymbol != "" {
				locSup.SupplierSymbol = tt.supplierSymbol
			}
			if tt.skipped {
				locSup.SupplierStatus = ill_db.SupplierStateSkippedPg
			}
			_, err := illRepo.SaveLocatedSupplier(appCtx, ill_db.SaveLocatedSupplierParams(locSup))
			if err != nil {
				t.Errorf("failed to update located supplier : %s", err)
			}
			url := "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
			req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
			req.Header.Add("Content-Type", "application/xml")
			client := &http.Client{}
			res, err := client.Do(req)
			if err != nil {
				t.Errorf("failed to send request to broker :%s", err)
			}
			assert.Equal(t, vcs.GetSignature(), res.Header.Get("Server"))
			if res.StatusCode != tt.status {
				t.Errorf("handler returned wrong status code: got '%v' want %v",
					res.StatusCode, tt.status)
			}
			body, _ := io.ReadAll(res.Body)
			if !strings.Contains(string(body), tt.contains) {
				t.Errorf("handler returned unexpected body: got '%v' want to contain %v",
					string(body), tt.contains)
			}
		})
	}
}

func TestIso18626PostRequestingMessageDBFailure(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoError, eventBussError, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "failed to process request\n", rr.Body.String())
}

func TestIso18626PostRequestingMessageNoReqId(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-no-reqid.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	errStatus := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), errStatus)
	errData := `<errorData>
 		<errorType>UnrecognisedDataValue</errorType>
 		<errorValue>requestingAgencyRequestId: cannot be empty</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

func TestIso18626PostRequestingMessageReqNotFound(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(&MockRepositoryReqNotFound{}, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	errStatus := "<messageStatus>ERROR</messageStatus>"
	assert.Contains(t, rr.Body.String(), errStatus)
	errData := `<errorData>
 		<errorType>UnrecognisedDataValue</errorType>
 		<errorValue>requestingAgencyRequestId: request with a given ID not found</errorValue>
	</errorData>`
	assert.Contains(t, norm(rr.Body.String()), norm(errData))
}

func TestIso18626PostRequestingMessageReqFailToSave(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-ok.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	handler.Iso18626PostHandler(&MockRepositoryReqExists{}, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Equal(t, "failed to process request\n", rr.Body.String())
}

func TestIso18626PostHandlerInvalidAction(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-invalid-action.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(new(MockRepositoryCurrentSupplier), eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<messageStatus>ERROR</messageStatus>")
	assert.Contains(t, rr.Body.String(), "<errorType>UnsupportedActionType</errorType>")
	assert.Contains(t, rr.Body.String(), "<errorValue>invalid action: WeCancelThisMessage</errorValue>")
}

func TestIso18626PostHandlerSupplierNotFound(t *testing.T) {
	data, _ := os.ReadFile("../testdata/reqmsg-invalid-action.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<messageStatus>ERROR</messageStatus>")
	assert.Contains(t, rr.Body.String(), "<errorType>UnrecognisedDataValue</errorType>")
	assert.Contains(t, rr.Body.String(), "<errorValue>supplyingAgencyId: supplying agency not found or invalid</errorValue>")
}

func TestIso18626PostHandlerInvalidStatus(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-invalid-status.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<messageStatus>ERROR</messageStatus>")
	assert.Contains(t, rr.Body.String(), "<errorType>UnrecognisedDataValue</errorType>")
	assert.Contains(t, rr.Body.String(), "<errorValue>invalid status: WeCouldLoan</errorValue>")
}

func TestIso18626PostHandlerInvalidReason(t *testing.T) {
	data, _ := os.ReadFile("../testdata/supmsg-invalid-reason.xml")
	req, _ := http.NewRequest("POST", "/", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()

	handler.Iso18626PostHandler(mockIllRepoSuccess, eventBussSuccess, dirAdapter, app.MAX_MESSAGE_SIZE)(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<messageStatus>ERROR</messageStatus>")
	assert.Contains(t, rr.Body.String(), "<errorType>UnsupportedReasonForMessageType</errorType>")
	assert.Contains(t, rr.Body.String(), "<errorValue>invalid reason: NoGoodReason</errorValue>")
}

type MockRepositoryOnlyPeersOK struct {
	mocks.MockIllRepositoryError
}

func (r *MockRepositoryOnlyPeersOK) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	return []ill_db.Peer{{
		ID:   "peer1",
		Name: symbols[0],
	}}, "", nil
}

type MockRepositoryReqNotFound struct {
	mocks.MockIllRepositoryError
}

func (r *MockRepositoryReqNotFound) GetIllTransactionByRequesterRequestId(ctx common.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, pgx.ErrNoRows
}

func (r *MockRepositoryReqNotFound) GetIllTransactionByRequesterRequestIdForUpdate(ctx common.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, pgx.ErrNoRows
}

func (r *MockRepositoryReqNotFound) WithTxFunc(ctx common.ExtendedContext, fn func(repo ill_db.IllRepo) error) error {
	return fn(r)
}

type MockRepositoryReqExists struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockRepositoryReqExists) SaveIllTransaction(ctx common.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, &pgconn.PgError{Code: "23505"}
}

func (r *MockRepositoryReqExists) WithTxFunc(ctx common.ExtendedContext, fn func(repo ill_db.IllRepo) error) error {
	return &pgconn.PgError{Code: "23505"}
}

type MockRepositoryOtherSupplier struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockRepositoryOtherSupplier) GetSelectedSupplierForIllTransaction(ctx common.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		SupplierSymbol: "ISIL:OTHER",
	}, nil
}

type MockRepositoryCurrentSupplier struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockRepositoryCurrentSupplier) GetSelectedSupplierForIllTransaction(ctx common.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		SupplierSymbol: "ISIL:SLNP_TWO_A",
	}, nil
}

func (r *MockRepositoryCurrentSupplier) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return fn(r)
}

type MockRepositorySupplierError struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockRepositorySupplierError) GetSelectedSupplierForIllTransaction(ctx common.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}
