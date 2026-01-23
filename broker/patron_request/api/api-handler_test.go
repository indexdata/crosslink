package prapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	params := proapi.GetPatronRequestsParams{
		Side:   string(prservice.SideLending),
		Symbol: &symbol,
	}
	handler.GetPatronRequests(rr, req, params)
	if status := rr.Code; status != http.StatusInternalServerError {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}
}

func TestPostPatronRequests(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	toCreate := proapi.PatronRequest{ID: "1", RequesterSymbol: &symbol}
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
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	toCreate := proapi.PatronRequest{ID: "1"}
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
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte("a\": v\"")))
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid character")
}

func TestDeletePatronRequestsIdNotFound(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeletePatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestDeletePatronRequestsId(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "3", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: proapi.Side(prservice.SideBorrowing)})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdNotFound(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPatronRequestsId(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "1", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActions(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: proapi.Side(prservice.SideBorrowing)})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "validate")
}

func TestGetPatronRequestsIdActionsNoSymbol(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Side: proapi.Side(prservice.SideBorrowing)})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestGetPatronRequestsIdActionsDbError(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "1", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: proapi.Side(prservice.SideBorrowing)})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActionsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: proapi.Side(prservice.SideLending)})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionNoSymbol(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: proapi.Side(prservice.SideBorrowing)})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPostPatronRequestsIdActionDbError(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "1", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: proapi.Side(prservice.SideBorrowing)})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdActionNotFoundBecauseOfSide(t *testing.T) {
	handler := NewApiHandler(new(PrRepoError), mockEventBus, common.NewTenant(""), 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: proapi.Side(prservice.SideLending)})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

type PrRepoError struct {
	mock.Mock
	pr_db.PgPrRepo
}

func (r *PrRepoError) WithTxFunc(ctx common.ExtendedContext, fn func(repo pr_db.PrRepo) error) error {
	return fn(r)
}

func (r *PrRepoError) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	switch id {
	case "2":
		return pr_db.PatronRequest{}, pgx.ErrNoRows
	case "3":
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
	return errors.New("DB error")
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
}
