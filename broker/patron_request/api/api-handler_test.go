package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var mockEventBus = new(MockEventBus)
var mockEventRepo = new(mocks.MockEventRepositorySuccess)
var symbol = "ISIL:REQ"
var lendingString = string(prservice.SideLending)
var proapiBorrowingSide = proapi.Side(prservice.SideBorrowing)
var proapiLendingSide = proapi.Side(prservice.SideLending)

func validIllRequest() iso18626.Request {
	return iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title: "Test title",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceType: iso18626.TypeServiceTypeCopy,
		},
	}
}

func TestGetId(t *testing.T) {
	assert.True(t, getId("") != "")
	assert.Equal(t, "id1", getId("id1"))
}

func TestGetDbText(t *testing.T) {
	text := "v1"
	result := getDbText(&text)
	assert.True(t, result.Valid)
	assert.Equal(t, "v1", result.String)

	result = getDbText(nil)
	assert.False(t, result.Valid)
}

func TestGetPatronRequests(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	params := proapi.GetPatronRequestsParams{
		Side:   &lendingString,
		Symbol: &symbol,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	params := proapi.GetPatronRequestsParams{
		Side: &lendingString,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsWithLimits(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	offset := proapi.Offset(10)
	limit := proapi.Limit(10)
	cql := "state = NEW"
	params := proapi.GetPatronRequestsParams{
		Side:   &lendingString,
		Symbol: &symbol,
		Offset: &offset,
		Limit:  &limit,
		Cql:    &cql,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsWithRequesterReqId(t *testing.T) {
	repo := new(PrRepoCapture)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	requesterReqID := "req-123"
	params := proapi.GetPatronRequestsParams{
		Side:           &lendingString,
		Symbol:         &symbol,
		RequesterReqId: &requesterReqID,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.cql) {
		assert.Contains(t, *repo.cql, "requester_req_id = req-123")
		assert.Contains(t, *repo.cql, "side = lending")
		assert.Contains(t, *repo.cql, "supplier_symbol = ISIL:REQ")
	}
}

func TestPostPatronRequests(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	id := "1"
	toCreate := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &symbol,
		IllRequest:      validIllRequest(),
	}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err, "failed to marshal patron request")
	req, err := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err, "failed to create request")
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	toCreate := proapi.PatronRequest{Id: "1"}
	jsonBytes, err := json.Marshal(toCreate)
	assert.NoError(t, err, "failed to marshal patron request")
	req, err := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err, "failed to create request")
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPostPatronRequestsInvalidJson(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte("a\": v\"")))
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid character")
}

func TestPostPatronRequestsInvalidIllRequestShape(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	jsonBytes := []byte(`{
		"id":"1",
		"requesterSymbol":"` + symbol + `",
		"illRequest":{"header":"invalid"}
	}`)
	req, err := http.NewRequest("POST", "/", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err, "failed to create request")
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "illRequest")
}

func TestDeletePatronRequestsIdNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeletePatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestDeletePatronRequestsIdError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "1", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestDeletePatronRequestsId(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "3", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestDeletePatronRequestsIdDeleted(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "4", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestGetPatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPatronRequestsId(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "1", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActions(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "{\"actions\":[]}\n", rr.Body.String())
}

func TestGetPatronRequestsIdActionsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdActionsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "1", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActionsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPostPatronRequestsIdActionDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "1", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdActionNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionErrorParsing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestGetPatronRequestsIdEventsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "3", proapi.GetPatronRequestsIdEventsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdEventsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "1", proapi.GetPatronRequestsIdEventsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdEventsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "3", proapi.GetPatronRequestsIdEventsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestGetPatronRequestsIdEventsErrorGettingEvents(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "3", proapi.GetPatronRequestsIdEventsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdNotificationsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdNotificationsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "1", proapi.GetPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdNotificationsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestGetPatronRequestsIdNotificationsErrorGettingEvents(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestParseAndValidateIllRequestAndBuildDbPatronRequest(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	creationTime := time.Now()
	id := uuid.NewString()
	reqWithID := &proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &symbol,
		IllRequest:      validIllRequest(),
	}

	illRequest, requesterReqID, err := handler.parseAndValidateIllRequest(ctx, reqWithID, creationTime)
	assert.NoError(t, err)
	assert.Equal(t, id, requesterReqID)
	pr := buildDbPatronRequest(reqWithID, nil, pgtype.Timestamp{Valid: true, Time: creationTime}, requesterReqID, illRequest)
	assert.Equal(t, id, pr.ID)
	assert.True(t, pr.CreatedAt.Valid)
	assert.True(t, pr.RequesterReqID.Valid)
	assert.Equal(t, id, pr.RequesterReqID.String)
	assert.False(t, pr.SupplierSymbol.Valid)

	reqWithoutID := &proapi.CreatePatronRequest{RequesterSymbol: &symbol}
	_, _, err = handler.parseAndValidateIllRequest(ctx, reqWithoutID, creationTime)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errInvalidPatronRequest))
}

func TestParseAndValidateIllRequestInvalidRequesterSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	invalidSymbol := "REQ"

	_, _, err := handler.parseAndValidateIllRequest(ctx, &proapi.CreatePatronRequest{RequesterSymbol: &invalidSymbol}, time.Now())
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errInvalidPatronRequest))
}

func TestParseAndValidateIllRequestInvalidBrokerSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	previousBrokerSymbol := brokerSymbol
	brokerSymbol = "BROKER"
	defer func() {
		brokerSymbol = previousBrokerSymbol
	}()

	_, _, err := handler.parseAndValidateIllRequest(ctx, &proapi.CreatePatronRequest{
		RequesterSymbol: &symbol,
		IllRequest:      validIllRequest(),
	}, time.Now())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid BROKER_SYMBOL")
}

func TestPostPatronRequestsIdNotificationsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPostPatronRequestsIdNotificationsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "1", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdNotificationsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdNotificationsErrorSavingNotification(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdNotificationsErrorBecauseOfBodyMissing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPostPatronRequestsIdNotificationsErrorBecauseOfBody(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	body := "{\"note"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestPostPatronRequestsIdNotificationsErrorFailedSendOnlyLogged(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositorySuccess), common.NewTenant(""), new(MockIso18626Handler), 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "4", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Contains(t, rr.Body.String(), "Say hello")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("PUT", "/", nil)
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "1", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, common.NewTenant(""), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptErrorReadingNotification(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n3", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n2", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptErrorBecauseOfBodyMissing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	req, _ := http.NewRequest("PUT", "/", nil)
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptErrorBecauseOfBody(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	body := "{\"receipt"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptFailedToSave(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), common.NewTenant(""), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n4", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

type PrRepoError struct {
	mock.Mock
	pr_db.PgPrRepo
	counter int64
}

type PrRepoCapture struct {
	PrRepoError
	cql *string
}

func (r *PrRepoCapture) ListPatronRequests(ctx common.ExtendedContext, args pr_db.ListPatronRequestsParams, cql *string) ([]pr_db.PatronRequest, int64, error) {
	r.cql = cql
	return []pr_db.PatronRequest{}, 0, nil
}

func (r *PrRepoError) WithTxFunc(ctx common.ExtendedContext, fn func(repo pr_db.PrRepo) error) error {
	return fn(r)
}

func (r *PrRepoError) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	switch id {
	case "2":
		return pr_db.PatronRequest{}, pgx.ErrNoRows
	case "3", "4":
		return pr_db.PatronRequest{ID: id, State: prservice.BorrowerStateNew, Side: prservice.SideBorrowing, RequesterSymbol: pgtype.Text{String: symbol, Valid: true}}, nil
	default:
		return pr_db.PatronRequest{}, errors.New("DB error")
	}
}

func (r *PrRepoError) ListPatronRequests(ctx common.ExtendedContext, args pr_db.ListPatronRequestsParams, cql *string) ([]pr_db.PatronRequest, int64, error) {
	return []pr_db.PatronRequest{}, 0, errors.New("DB error")
}

func (r *PrRepoError) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	return pr_db.PatronRequest{}, errors.New("DB error")
}

func (r *PrRepoError) CreatePatronRequest(ctx common.ExtendedContext, params pr_db.CreatePatronRequestParams) (pr_db.PatronRequest, error) {
	return pr_db.PatronRequest{}, errors.New("DB error")
}

func (r *PrRepoError) DeletePatronRequest(ctx common.ExtendedContext, id string) error {
	if id == "4" {
		return nil
	}
	return errors.New("DB error")
}

func (r *PrRepoError) GetNextHrid(ctx common.ExtendedContext, prefix string) (string, error) {
	r.counter++
	return strings.ToUpper(prefix) + "-" + strconv.FormatInt(r.counter, 10), nil
}

func (r *PrRepoError) GetNotificationsByPrId(ctx common.ExtendedContext, params pr_db.GetNotificationsByPrIdParams) ([]pr_db.Notification, int64, error) {
	return []pr_db.Notification{}, int64(0), errors.New("DB error")
}

func (r *PrRepoError) SaveNotification(ctx common.ExtendedContext, params pr_db.SaveNotificationParams) (pr_db.Notification, error) {
	if params.ID == "n4" || params.PrID == "3" {
		return pr_db.Notification{}, errors.New("DB error")
	}
	return pr_db.Notification(params), nil
}

func (r *PrRepoError) GetNotificationById(ctx common.ExtendedContext, id string) (pr_db.Notification, error) {
	switch id {
	case "n2":
		return pr_db.Notification{}, pgx.ErrNoRows
	case "n3":
		return pr_db.Notification{ID: id}, errors.New("DB error")
	default:
		return pr_db.Notification{ID: id}, nil
	}
}

type MockIso18626Handler struct {
	mock.Mock
	handler.Iso18626Handler
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
}

func (h *MockEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventDomain events.EventDomain, parentId *string) (string, error) {
	return "", errors.New("DB error")
}

func ptr(value string) *string {
	return &value
}
