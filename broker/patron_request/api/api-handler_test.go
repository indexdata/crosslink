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

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var mockEventBus = new(MockEventBus)
var symbol = "ISIL:REQ"
var lendingString = string(prservice.SideLending)
var proapiBorrowingSide = proapi.Side(prservice.SideBorrowing)
var proapiLendingSide = proapi.Side(prservice.SideLending)

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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
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

func TestPostPatronRequests(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	toCreate := proapi.PatronRequest{Id: "1", RequesterSymbol: &symbol}
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte("a\": v\"")))
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid character")
}

func TestDeletePatronRequestsIdNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeletePatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestDeletePatronRequestsIdError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "1", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestDeletePatronRequestsId(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "3", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestDeletePatronRequestsIdDeleted(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "4", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestGetPatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPatronRequestsId(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "1", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActions(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "validate")
}

func TestGetPatronRequestsIdActionsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdActionsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "1", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActionsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPostPatronRequestsIdActionDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "1", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdActionNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionErrorParsing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestToDbPatronRequest(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	id := uuid.NewString()

	pr, err := handler.toDbPatronRequest(ctx, proapi.CreatePatronRequest{Id: &id, RequesterSymbol: &symbol}, nil)
	assert.NoError(t, err)
	assert.Equal(t, id, pr.ID)
	assert.True(t, pr.Timestamp.Valid)

	pr, err = handler.toDbPatronRequest(ctx, proapi.CreatePatronRequest{RequesterSymbol: &symbol}, nil)
	assert.NoError(t, err)
	assert.Equal(t, "REQ-1", pr.ID)
	assert.True(t, pr.Timestamp.Valid)
}

type PrRepoError struct {
	mock.Mock
	pr_db.PgPrRepo
	counter int64
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
func (r *PrRepoError) SavePatronRequest(ctx common.ExtendedContext, params pr_db.SavePatronRequestParams) (pr_db.PatronRequest, error) {
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

type MockEventBus struct {
	mock.Mock
	events.EventBus
}
