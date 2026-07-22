package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/directory"
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

func TestToApiPatronRequestOmitsOwnerLinksWithoutDetectedSymbol(t *testing.T) {
	now := time.Now()
	req := httptest.NewRequest("GET", "http://localhost/patron_requests/pr-1", nil)
	pr := pr_db.PatronRequest{
		ID:        "pr-1",
		CreatedAt: pgtype.Timestamp{Valid: true, Time: now},
		State:     pr_db.PatronRequestState("NEW"),
		Side:      prservice.SideBorrowing,
		RequesterReqID: pgtype.Text{
			String: "REQ-1",
			Valid:  true,
		},
		// Borrowing side owner symbol is requester; keep it invalid to assert no fallback to supplier.
		RequesterSymbol: pgtype.Text{Valid: false},
		SupplierSymbol: pgtype.Text{
			String: "ISIL:SUP",
			Valid:  true,
		},
	}

	prView := patronRequestSearchViewFromPatronRequest(pr, true)
	prView.UnreadNotificationsCount = 3
	apiPr := toApiPatronRequest(req, prView)
	assert.True(t, apiPr.HasCost)
	assert.Equal(t, int64(3), apiPr.UnreadNotificationsCount)
	assert.Nil(t, apiPr.NotificationsLink)
	assert.Nil(t, apiPr.ItemsLink)
	assert.Nil(t, apiPr.AvailableActionsLink)
	assert.Nil(t, apiPr.EventsLink)
	assert.NotNil(t, apiPr.IllTransactionLink)
	assert.Contains(t, *apiPr.IllTransactionLink, "requester_req_id=REQ-1")
}

func TestToApiPatronRequestOmitsIllTransactionLinkWithoutRequesterReqID(t *testing.T) {
	now := time.Now()
	req := httptest.NewRequest("GET", "http://localhost/patron_requests/pr-2", nil)
	pr := pr_db.PatronRequest{
		ID:        "pr-2",
		CreatedAt: pgtype.Timestamp{Valid: true, Time: now},
		State:     pr_db.PatronRequestState("NEW"),
		Side:      prservice.SideBorrowing,
		RequesterSymbol: pgtype.Text{
			String: "ISIL:REQ",
			Valid:  true,
		},
		RequesterReqID: pgtype.Text{Valid: false},
	}

	apiPr := toApiPatronRequest(req, patronRequestSearchViewFromPatronRequest(pr, false))
	assert.NotNil(t, apiPr.NotificationsLink)
	assert.NotNil(t, apiPr.ItemsLink)
	assert.NotNil(t, apiPr.AvailableActionsLink)
	assert.NotNil(t, apiPr.EventsLink)
	assert.Nil(t, apiPr.IllTransactionLink)
}

func TestToApiPatronRequestSurfacesInternalNote(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/patron_requests/pr-1", nil)
	pr := pr_db.PatronRequest{
		ID:           "pr-1",
		InternalNote: pgtype.Text{String: "staff note", Valid: true},
		StateModel:   "returnables",
	}
	apiPr := toApiPatronRequest(req, patronRequestSearchViewFromPatronRequest(pr, false))
	assert.Equal(t, "returnables", apiPr.StateModel)
	if assert.NotNil(t, apiPr.InternalNote) {
		assert.Equal(t, "staff note", *apiPr.InternalNote)
	}
}

func patronRequestSearchViewFromPatronRequest(pr pr_db.PatronRequest, hasCost bool) pr_db.PatronRequestSearchView {
	return pr_db.PatronRequestSearchView{
		ID:                pr.ID,
		CreatedAt:         pr.CreatedAt,
		IllRequest:        pr.IllRequest,
		State:             pr.State,
		Side:              pr.Side,
		Patron:            pr.Patron,
		RequesterSymbol:   pr.RequesterSymbol,
		SupplierSymbol:    pr.SupplierSymbol,
		Tenant:            pr.Tenant,
		RequesterReqID:    pr.RequesterReqID,
		NeedsAttention:    pr.NeedsAttention,
		LastAction:        pr.LastAction,
		LastActionOutcome: pr.LastActionOutcome,
		LastActionResult:  pr.LastActionResult,
		Items:             pr.Items,
		Language:          pr.Language,
		TerminalState:     pr.TerminalState,
		UpdatedAt:         pr.UpdatedAt,
		IllResponse:       pr.IllResponse,
		InternalNote:      pr.InternalNote,
		StateModel:        pr.StateModel,
		HasCost:           hasCost,
	}
}

func TestGetPatronRequests(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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

func TestGetPatronRequestsFacetsDBError(t *testing.T) {
	facets := proapi.Facets{"requester_symbol"}
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	params := proapi.GetPatronRequestsParams{
		Facets: &facets,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsFacetsUnsupported(t *testing.T) {
	facets := proapi.Facets{"nosuch"}
	handler := NewPrApiHandler(new(PrRepoFacetsUnsupported), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	params := proapi.GetPatronRequestsParams{
		Facets: &facets,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "nosuch")
}

func TestGetPatronRequestsNoSymbol(t *testing.T) {
	repo := new(PrRepoCapture)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	params := proapi.GetPatronRequestsParams{
		Side: &lendingString,
	}
	handler.GetPatronRequests(rr, req, params)
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.pgcql) {
		assert.Contains(t, repo.pgcql.GetWhereClause(), "side =")
		assert.NotContains(t, repo.pgcql.GetWhereClause(), "supplier_symbol =")
		assert.NotContains(t, repo.pgcql.GetWhereClause(), "requester_symbol =")
	}
}

func TestGetPatronRequestsWithLimits(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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
	if assert.NotNil(t, repo.pgcql) {
		assert.Contains(t, repo.pgcql.GetWhereClause(), "requester_req_id =")
		assert.Contains(t, repo.pgcql.GetWhereClause(), "side =")
		assert.Contains(t, repo.pgcql.GetWhereClause(), "supplier_symbol =")
	}
}

func TestGetPatronRequestsWithSymbolNoSideGroupsOwnerRestriction(t *testing.T) {
	repo := new(PrRepoCapture)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	cql := "id = pr-1"
	params := proapi.GetPatronRequestsParams{
		Symbol: &symbol,
		Cql:    &cql,
	}

	handler.GetPatronRequests(rr, req, params)

	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.pgcql) {
		assert.Equal(t, "id = $3 AND ((side = $4 AND supplier_symbol = $5) OR (side = $6 AND requester_symbol = $7))", repo.pgcql.GetWhereClause())
	}
}

func TestPostPatronRequests(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", bytes.NewBuffer([]byte("a\": v\"")))
	rr := httptest.NewRecorder()
	tenant := proapi.Tenant("test-lib")
	handler.PostPatronRequests(rr, req, proapi.PostPatronRequestsParams{XOkapiTenant: &tenant})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid character")
}

func TestPostPatronRequestsInvalidIllRequestShape(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func internalNoteBody(t *testing.T, note *string) *bytes.Buffer {
	jsonBytes, err := json.Marshal(proapi.UpdateInternalNote{InternalNote: note})
	assert.NoError(t, err)
	return bytes.NewBuffer(jsonBytes)
}

func TestPutPatronRequestsIdInternalNoteSetsNote(t *testing.T) {
	repo := new(PrRepoError)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	note := "hello staff"
	req, _ := http.NewRequest("PUT", "/", internalNoteBody(t, &note))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdInternalNote(rr, req, "4", proapi.PutPatronRequestsIdInternalNoteParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, pgtype.Text{Valid: true, String: "hello staff"}, repo.lastInternalNote)
}

func TestPutPatronRequestsIdInternalNoteClearsOnEmpty(t *testing.T) {
	repo := new(PrRepoError)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	empty := ""
	req, _ := http.NewRequest("PUT", "/", internalNoteBody(t, &empty))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdInternalNote(rr, req, "4", proapi.PutPatronRequestsIdInternalNoteParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.False(t, repo.lastInternalNote.Valid, "empty string should clear to NULL")
}

func TestPutPatronRequestsIdInternalNoteClearsOnAbsent(t *testing.T) {
	repo := new(PrRepoError)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString("{}"))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdInternalNote(rr, req, "4", proapi.PutPatronRequestsIdInternalNoteParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.False(t, repo.lastInternalNote.Valid, "absent field should clear to NULL")
}

func TestPutPatronRequestsIdInternalNoteNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	note := "x"
	req, _ := http.NewRequest("PUT", "/", internalNoteBody(t, &note))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdInternalNote(rr, req, "2", proapi.PutPatronRequestsIdInternalNoteParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeletePatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "2", proapi.DeletePatronRequestsIdParams{})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestDeletePatronRequestsIdError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "1", proapi.DeletePatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestDeletePatronRequestsId(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "3", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestDeletePatronRequestsIdDeleted(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.DeletePatronRequestsId(rr, req, "4", proapi.DeletePatronRequestsIdParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestGetPatronRequestsIdMissingSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestGetPatronRequestsIdNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "2", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetPatronRequestsId(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsId(rr, req, "1", proapi.GetPatronRequestsIdParams{Symbol: &symbol})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActions(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "{\"actions\":[]}\n", rr.Body.String())
}

func TestGetPatronRequestsIdActionsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "{\"actions\":[]}\n", rr.Body.String())
}

func TestGetPatronRequestsIdActionsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "1", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdActionsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdActions(rr, req, "3", proapi.GetPatronRequestsIdActionsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPostPatronRequestsIdActionDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "1", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdActionNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdActionErrorParsing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestPostPatronRequestsIdActionStoresTenantUserInInvokeTask(t *testing.T) {
	tenantResolver := tenant.NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}")
	eventBus := new(MockEventBusCapture)
	handler := NewPrApiHandler(new(PrRepoOkapiOwner), eventBus, mockEventRepo, tenantResolver, nil, 10)
	handler.SetActionTaskProcessor(&MockActionTaskProcessor{})

	reqBody := `{"action":"` + string(prservice.BorrowerActionSendRequest) + `"}`
	req, _ := http.NewRequest("POST", "/broker/patron_requests/3/action", strings.NewReader(reqBody))
	req.Header.Set("X-Okapi-Tenant", "tenant1")
	req.Header.Set("X-Okapi-User-Id", "okapi-user-1")
	req.Header.Set("X-Okapi-Token", "header.eyJzdWIiOiJva2FwaS1zdWJqZWN0IiwidXNlcl9pZCI6Im9rYXBpLXVzZXItMSJ9.signature")
	rr := httptest.NewRecorder()

	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "okapi-subject", eventBus.lastData.User)
}

func TestPostPatronRequestsIdActionReturnsExclusiveTaskError(t *testing.T) {
	eventBus := new(MockEventBusCapture)
	handler := NewPrApiHandler(new(PrRepoOkapiOwner), eventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	handler.SetActionTaskProcessor(&MockActionTaskProcessorExclusiveError{})

	reqBody := `{"action":"` + string(prservice.BorrowerActionSendRequest) + `"}`
	req, _ := http.NewRequest("POST", "/", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: &proapiBorrowingSide})

	assert.Equal(t, http.StatusOK, rr.Code)
	var result proapi.ActionResult
	err := json.Unmarshal(rr.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.Equal(t, string(events.EventStatusError), result.Result)
	assert.Equal(t, prservice.ActionOutcomeFailure, result.Outcome)
	if assert.NotNil(t, result.Message) {
		assert.Equal(t, "another invoke-action task in progress", *result.Message)
	}
}

func TestPostPatronRequestsIdActionRejectsTerminate(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoOkapiOwner), new(MockEventBusCapture), mockEventRepo, tenant.NewResolver(), nil, 10)
	handler.SetActionTaskProcessor(&MockActionTaskProcessor{})

	reqBody := `{"action":"` + string(prservice.TerminateAction) + `"}`
	req, _ := http.NewRequest("POST", "/", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.PostPatronRequestsIdAction(rr, req, "3", proapi.PostPatronRequestsIdActionParams{Side: &proapiBorrowingSide})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "Action terminate is not allowed")
}

func TestPostPatronRequestsIdTerminateStoresTenantUserAndActionInInvokeTask(t *testing.T) {
	tenantResolver := tenant.NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}")
	eventBus := new(MockEventBusCapture)
	handler := NewPrApiHandler(new(PrRepoOkapiOwner), eventBus, mockEventRepo, tenantResolver, nil, 10)
	handler.SetActionTaskProcessor(&MockActionTaskProcessor{})

	req, _ := http.NewRequest("POST", "/broker/patron_requests/3/terminate", nil)
	req.Header.Set("X-Okapi-Tenant", "tenant1")
	req.Header.Set("X-Okapi-User-Id", "okapi-user-1")
	req.Header.Set("X-Okapi-Token", "header.eyJzdWIiOiJva2FwaS1zdWJqZWN0IiwidXNlcl9pZCI6Im9rYXBpLXVzZXItMSJ9.signature")
	rr := httptest.NewRecorder()

	handler.PostPatronRequestsIdTerminate(rr, req, "3", proapi.PostPatronRequestsIdTerminateParams{Side: &proapiBorrowingSide})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, events.EventNameInvokeAction, eventBus.lastEventName)
	assert.NotNil(t, eventBus.lastData.Action)
	assert.Equal(t, prservice.TerminateAction, *eventBus.lastData.Action)
	assert.Equal(t, "okapi-subject", eventBus.lastData.User)
}

func TestPostPatronRequestsIdTerminateRejectsTerminal(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoTerminal), new(MockEventBusCapture), mockEventRepo, tenant.NewResolver(), nil, 10)
	handler.SetActionTaskProcessor(&MockActionTaskProcessor{})

	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()

	handler.PostPatronRequestsIdTerminate(rr, req, "3", proapi.PostPatronRequestsIdTerminateParams{Symbol: &symbol, Side: &proapiBorrowingSide})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "already terminal")
}

func TestGetPatronRequestsIdEventsNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "3", proapi.GetPatronRequestsIdEventsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestGetPatronRequestsIdEventsRejectsSyntheticIDs(t *testing.T) {
	handler := NewPrApiHandler(nil, nil, nil, nil, nil, 10)
	for _, id := range []string{events.DEFAULT_ILL_TRANSACTION_ID, events.DEFAULT_PATRON_REQUEST_ID} {
		t.Run(id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/patron_requests/"+id+"/events", nil)
			rr := httptest.NewRecorder()
			handler.GetPatronRequestsIdEvents(rr, req, id, proapi.GetPatronRequestsIdEventsParams{})
			assert.Equal(t, http.StatusBadRequest, rr.Code)
			assert.Contains(t, rr.Body.String(), "synthetic IDs are not allowed")
		})
	}
}

func TestGetPatronRequestsIdEventsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "1", proapi.GetPatronRequestsIdEventsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdEventsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "3", proapi.GetPatronRequestsIdEventsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestGetPatronRequestsIdEventsErrorGettingEvents(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdEvents(rr, req, "3", proapi.GetPatronRequestsIdEventsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdNotificationsNoSymbol(t *testing.T) {
	repo := &PrRepoNotificationsCapture{}
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.lastParams) {
		assert.Equal(t, "3", repo.lastParams.PrID)
	}
}

func TestGetPatronRequestsIdNotificationsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "1", proapi.GetPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdNotificationsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestGetPatronRequestsIdNotificationsErrorGettingEvents(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetPatronRequestsIdNotificationsWithKindFilter(t *testing.T) {
	repo := &PrRepoNotificationsCapture{
		notifications: []pr_db.Notification{
			{
				ID:         "n-condition-1",
				PrID:       "3",
				FromSymbol: "ISIL:BROKER",
				ToSymbol:   "ISIL:REQ",
				Direction:  pr_db.NotificationDirectionReceived,
				Kind:       pr_db.NotificationKindCondition,
				Condition: pgtype.Text{
					String: "NoReproduction",
					Valid:  true,
				},
				Note: pgtype.Text{
					String: "please do not copy",
					Valid:  true,
				},
				CreatedAt: pgtype.Timestamp{
					Time:  time.Now(),
					Valid: true,
				},
			},
		},
		fullCount: 1,
	}
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	kind := proapi.GetPatronRequestsIdNotificationsParamsKind(proapi.PrNotificationKindCondition)
	limit := proapi.Limit(5)
	offset := proapi.Offset(2)
	handler.GetPatronRequestsIdNotifications(rr, req, "3", proapi.GetPatronRequestsIdNotificationsParams{
		Symbol: &symbol,
		Side:   &proapiBorrowingSide,
		Kind:   &kind,
		Limit:  &limit,
		Offset: &offset,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.lastParams) {
		assert.Equal(t, "3", repo.lastParams.PrID)
		assert.Equal(t, int32(5), repo.lastParams.Limit)
		assert.Equal(t, int32(2), repo.lastParams.Offset)
		assert.Equal(t, "condition", repo.lastParams.Kind)
	}

	var response proapi.PrNotifications
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	if assert.Len(t, response.Items, 1) {
		assert.Equal(t, proapi.PrNotificationKindCondition, response.Items[0].Kind)
		assert.Equal(t, "NoReproduction", *response.Items[0].Condition)
		assert.Equal(t, "please do not copy", *response.Items[0].Note)
	}
	assert.Equal(t, int64(1), response.About.Count)
}

func TestParseAndValidateIllRequestAndBuildDbPatronRequest(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	creationTime := time.Now()
	id := uuid.NewString()
	patron := "user-1"
	reqWithID := &proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &symbol,
		IllRequest:      validIllRequest(),
		Patron:          &patron,
	}

	illRequest, requesterReqID, err := handler.parseAndValidateIllRequest(ctx, reqWithID, creationTime)
	assert.NoError(t, err)
	assert.Equal(t, id, requesterReqID)
	pr := buildDbPatronRequest(reqWithID, nil, pgtype.Timestamp{Valid: true, Time: creationTime}, requesterReqID, illRequest, prservice.BorrowerStateNew, "returnables")
	assert.Equal(t, id, pr.ID)
	assert.True(t, pr.CreatedAt.Valid)
	assert.True(t, pr.RequesterReqID.Valid)
	assert.Equal(t, id, pr.RequesterReqID.String)
	assert.False(t, pr.SupplierSymbol.Valid)
	assert.Equal(t, patron, pr.Patron.String)
	assert.Equal(t, patron, pr.IllRequest.PatronInfo.PatronId)
	assert.Equal(t, "returnables", pr.StateModel)

	reqWithoutID := &proapi.CreatePatronRequest{RequesterSymbol: &symbol}
	_, _, err = handler.parseAndValidateIllRequest(ctx, reqWithoutID, creationTime)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errInvalidPatronRequest))
}

func TestParseAndValidateIllRequestInvalidRequesterSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	invalidSymbol := "REQ"

	_, _, err := handler.parseAndValidateIllRequest(ctx, &proapi.CreatePatronRequest{RequesterSymbol: &invalidSymbol}, time.Now())
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errInvalidPatronRequest))
}

func TestParseAndValidateIllRequestInvalidBrokerSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
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
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPostPatronRequestsIdNotificationsDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "1", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdNotificationsNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPostPatronRequestsIdNotificationsErrorSavingNotification(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPostPatronRequestsIdNotificationsErrorBecauseOfBodyMissing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPostPatronRequestsIdNotificationsErrorBecauseOfBody(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"note"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestPostPatronRequestsIdNotificationsErrorBecauseOfMissingNote(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "3", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "note is required")
}

func TestPostPatronRequestsIdNotificationsErrorFailedSendOnlyLogged(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositorySuccess), tenant.NewResolver(), new(MockIso18626Handler), 10)
	body := "{\"note\": \"Say hello\"}"
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PostPatronRequestsIdNotifications(rr, req, "4", proapi.PostPatronRequestsIdNotificationsParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.Contains(t, rr.Body.String(), "Say hello")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptNoSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", nil)
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptDbError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "1", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptNotFoundBecauseOfSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiLendingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptErrorReadingNotification(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n3", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n2", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptErrorBecauseOfBodyMissing(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", nil)
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptErrorBecauseOfBody(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"receipt"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n1", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "unexpected EOF")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptPrDoesNotOwn(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "3", "n4", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "not found")
}

func TestPutPatronRequestsIdNotificationsNotificationIdReceiptFailedToSave(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	body := "{\"receipt\": \"SEEN\"}"
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsIdNotificationsNotificationIdReceipt(rr, req, "4", "4", proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams{Symbol: &symbol, Side: &proapiBorrowingSide})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestGetStateModelBatchActions(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, new(mocks.MockEventRepositoryError), tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.GetStateModelBatchActions(rr, req, proapi.GetStateModelBatchActionsParams{})
	assert.Equal(t, http.StatusOK, rr.Code)
	var actions []proapi.BatchActionDefault
	err := json.Unmarshal(rr.Body.Bytes(), &actions)
	assert.NoError(t, err)
	assert.Len(t, actions, 4)
	// Clients key translated titles off titleKey, so each default needs a distinct one.
	seen := map[string]bool{}
	for _, action := range actions {
		assert.NotEmpty(t, action.TitleKey)
		assert.False(t, seen[action.TitleKey], "duplicate titleKey %s", action.TitleKey)
		seen[action.TitleKey] = true
	}
}

type PrRepoError struct {
	mock.Mock
	pr_db.PgPrRepo
	counter          int64
	lastInternalNote pgtype.Text
}

type PrRepoOkapiOwner struct {
	PrRepoError
}

type PrRepoTerminal struct {
	PrRepoError
}

func (r *PrRepoOkapiOwner) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	if id == "3" {
		return pr_db.PatronRequest{ID: id, State: prservice.BorrowerStateNeedsReview, Side: prservice.SideBorrowing, RequesterSymbol: pgtype.Text{String: "ISIL:DK-TENANT1", Valid: true}}, nil
	}
	return r.PrRepoError.GetPatronRequestById(ctx, id)
}

func (r *PrRepoTerminal) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	if id == "3" {
		return pr_db.PatronRequest{ID: id, State: prservice.BorrowerStateCompleted, Side: prservice.SideBorrowing, RequesterSymbol: pgtype.Text{String: symbol, Valid: true}, TerminalState: true}, nil
	}
	return r.PrRepoError.GetPatronRequestById(ctx, id)
}

func (r *PrRepoOkapiOwner) GetPatronRequestSearchView(ctx common.ExtendedContext, id string) (pr_db.PatronRequestSearchView, error) {
	pr, err := r.GetPatronRequestById(ctx, id)
	return patronRequestSearchViewFromPatronRequest(pr, false), err
}

type PrRepoCapture struct {
	PrRepoError
	pgcql pgcql.Query
}

type PrRepoNotificationsCapture struct {
	PrRepoError
	lastParams    *pr_db.GetNotificationsByPrIdParams
	notifications []pr_db.Notification
	fullCount     int64
}

func (r *PrRepoCapture) ListPatronRequests(ctx common.ExtendedContext, args pr_db.ListPatronRequestsParams, pgcql pgcql.Query) ([]pr_db.PatronRequest, int64, error) {
	r.pgcql = pgcql
	return []pr_db.PatronRequest{}, 0, nil
}

func (r *PrRepoCapture) ListPatronRequestsSearchView(ctx common.ExtendedContext, args pr_db.ListPatronRequestsParams, pgcql pgcql.Query) ([]pr_db.PatronRequestSearchView, int64, error) {
	r.pgcql = pgcql
	return []pr_db.PatronRequestSearchView{}, 0, nil
}

func (r *PrRepoNotificationsCapture) GetNotificationsByPrId(ctx common.ExtendedContext, params pr_db.GetNotificationsByPrIdParams) ([]pr_db.Notification, int64, error) {
	paramsCopy := params
	r.lastParams = &paramsCopy
	return r.notifications, r.fullCount, nil
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
	case "5":
		return pr_db.PatronRequest{ID: id, State: prservice.BorrowerStateNeedsReview, Side: prservice.SideBorrowing, RequesterSymbol: pgtype.Text{String: symbol, Valid: true}}, nil
	default:
		return pr_db.PatronRequest{}, errors.New("DB error")
	}
}

func (r *PrRepoError) GetPatronRequestSearchView(ctx common.ExtendedContext, id string) (pr_db.PatronRequestSearchView, error) {
	pr, err := r.GetPatronRequestById(ctx, id)
	return patronRequestSearchViewFromPatronRequest(pr, false), err
}

func (r *PrRepoError) ListPatronRequests(ctx common.ExtendedContext, args pr_db.ListPatronRequestsParams, pgcql pgcql.Query) ([]pr_db.PatronRequest, int64, error) {
	return []pr_db.PatronRequest{}, 0, errors.New("DB error")
}

func (r *PrRepoError) ListPatronRequestsSearchView(ctx common.ExtendedContext, args pr_db.ListPatronRequestsParams, pgcql pgcql.Query) ([]pr_db.PatronRequestSearchView, int64, error) {
	return []pr_db.PatronRequestSearchView{}, 0, errors.New("DB error")
}

func (r *PrRepoError) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	return pr_db.PatronRequest{}, errors.New("DB error")
}

func (r *PrRepoError) UpdatePatronRequestInternalNote(ctx common.ExtendedContext, id string, internalNote pgtype.Text) error {
	r.lastInternalNote = internalNote
	if id == "4" {
		return nil
	}
	return errors.New("DB error")
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
	if params.ID == "4" || params.PrID == "3" {
		return pr_db.Notification{}, errors.New("DB error")
	}
	return pr_db.Notification(params), nil
}

func (r *PrRepoError) GetNotificationById(ctx common.ExtendedContext, id string) (pr_db.Notification, error) {
	switch id {
	case "n2":
		return pr_db.Notification{}, pgx.ErrNoRows
	case "n3":
		return pr_db.Notification{}, errors.New("DB error")
	default:
		return pr_db.Notification{ID: id, PrID: id}, nil
	}
}

func (r *PrRepoError) GetPatronRequestsFacets(_ common.ExtendedContext, _ []string, _ pgcql.Query) ([]pr_db.Facet, error) {
	return nil, errors.New("DB error")
}

type PrRepoFacetsUnsupported struct {
	PrRepoCapture
}

func (r *PrRepoFacetsUnsupported) GetPatronRequestsFacets(_ common.ExtendedContext, _ []string, _ pgcql.Query) ([]pr_db.Facet, error) {
	return nil, fmt.Errorf("%w: nosuch", pr_db.ErrUnsupportedFacet)
}

type MockIso18626Handler struct {
	mock.Mock
	handler.Iso18626Handler
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
}

func (h *MockEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventDomain events.EventDomain, parentId *string, target events.SignalTarget) (string, error) {
	return "", errors.New("DB error")
}

type MockEventBusCapture struct {
	MockEventBus
	lastEventName events.EventName
	lastData      events.EventData
}

func (h *MockEventBusCapture) CreateTask(id string, eventName events.EventName, data events.EventData, eventDomain events.EventDomain, parentId *string, target events.SignalTarget) (string, error) {
	h.lastEventName = eventName
	h.lastData = data
	return uuid.NewString(), nil
}

type MockActionTaskProcessor struct{}

func (m *MockActionTaskProcessor) ProcessInvokeActionTask(ctx common.ExtendedContext, event events.Event) (events.Event, error) {
	return events.Event{
		ID:          event.ID,
		EventStatus: events.EventStatusSuccess,
		ResultData: events.EventResult{
			CommonEventData: events.CommonEventData{
				ActionResult: &events.ActionResult{Outcome: prservice.ActionOutcomeSuccess},
			},
		},
	}, nil
}

type MockActionTaskProcessorExclusiveError struct{}

func (m *MockActionTaskProcessorExclusiveError) ProcessInvokeActionTask(ctx common.ExtendedContext, event events.Event) (events.Event, error) {
	return events.Event{
		ID:          event.ID,
		EventStatus: events.EventStatusError,
		ResultData: events.EventResult{
			CommonEventData: events.CommonEventData{
				EventError: &events.EventError{Message: "another invoke-action task in progress"},
			},
		},
	}, nil
}

// --- metadataUpdate tests ---

// mockLookupCreator controls what GetAdapter returns when no globalLookupAdapter is pre-set.
type mockLookupCreator struct {
	adapter catalog.LookupAdapter
	err     error
}

func (m *mockLookupCreator) GetAdapter(peer ill_db.Peer) (catalog.LookupAdapter, error) {
	return m.adapter, m.err
}

// peerWithMetadataMode builds a Peer whose CustomData carries the given MetadataUpdateMode.
// Pass nil to leave CatalogConfig absent entirely.
func peerWithMetadataMode(mode *directory.MetadataUpdateMode) ill_db.Peer {
	var cc *directory.CatalogConfig
	if mode != nil {
		cc = &directory.CatalogConfig{MetadataUpdateMode: mode}
	}
	return ill_db.Peer{
		CustomData: directory.Entry{Name: "test-peer", CatalogConfig: cc},
	}
}

// lookupFactoryWithAdapter creates a LookupAdapterFactory that returns the given adapter directly.
func lookupFactoryWithAdapter(adapter catalog.LookupAdapter) *service.LookupAdapterFactory {
	return service.NewLookupAdapterFactory(nil, nil, "", adapter, nil)
}

func TestMetadataUpdateNoFactory(t *testing.T) {
	h := PatronRequestApiHandler{}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := h.metadataUpdate(ctx, &iso18626.Request{}, ill_db.Peer{})
	assert.NoError(t, err)
}

func TestMetadataUpdateAdapterInitError(t *testing.T) {
	creator := &mockLookupCreator{err: errors.New("adapter init failed")}
	factory := service.NewLookupAdapterFactory(nil, nil, "", nil, creator)
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := h.metadataUpdate(ctx, &iso18626.Request{}, ill_db.Peer{})
	assert.ErrorContains(t, err, "failed to get lookup adapter")
}

func TestMetadataUpdateNilLookupAdapter(t *testing.T) {
	creator := &mockLookupCreator{} // returns nil adapter, nil error
	factory := service.NewLookupAdapterFactory(nil, nil, "", nil, creator)
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := h.metadataUpdate(ctx, &iso18626.Request{}, ill_db.Peer{})
	assert.NoError(t, err)
}

func TestMetadataUpdateNoCatalogConfig(t *testing.T) {
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	peer := peerWithMetadataMode(nil) // CatalogConfig absent → mode stays None
	err := h.metadataUpdate(ctx, &iso18626.Request{}, peer)
	assert.NoError(t, err)
}

func TestMetadataUpdateModeNone(t *testing.T) {
	mode := directory.None
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := h.metadataUpdate(ctx, &iso18626.Request{}, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
}

func TestMetadataUpdateMetadataLookupError(t *testing.T) {
	mode := directory.Merge
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Err: errors.New("lookup failed")})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := h.metadataUpdate(ctx, &iso18626.Request{}, peerWithMetadataMode(&mode))
	assert.ErrorContains(t, err, "failed to perform lookup for patron request")
}

func TestMetadataUpdateMergePopulatesEmptyFields(t *testing.T) {
	mode := directory.Merge
	meta := catalog.Metadata{Title: "Catalog Title", Author: "Jane Doe"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{} // empty bib info
	err := h.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Catalog Title", req.BibliographicInfo.Title)
	assert.Equal(t, "Jane Doe", req.BibliographicInfo.Author)
}

func TestMetadataUpdateMergePreservesExistingFields(t *testing.T) {
	mode := directory.Merge
	meta := catalog.Metadata{Title: "Catalog Title"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{Title: "Existing Title"},
	}
	err := h.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Existing Title", req.BibliographicInfo.Title) // not overwritten
}

func TestMetadataUpdateAutoModeWithIdentifierReplaces(t *testing.T) {
	mode := directory.Auto
	meta := catalog.Metadata{Title: "Catalog Title", Author: "Catalog Author", Isbn: "1234567890"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title:                  "Old Title",
			SupplierUniqueRecordId: "record-123", // non-empty → Auto resolves to Replace
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifier:     "0987654321",
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
				},
			},
		},
	}
	err := h.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Catalog Title", req.BibliographicInfo.Title)                                              // replaced
	assert.Equal(t, "Catalog Author", req.BibliographicInfo.Author)                                            // replaced
	assert.Equal(t, "1234567890", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier)    // replaced
	assert.Equal(t, "ISBN", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text) // replaced
}

func TestMetadataUpdateAutoModeWithoutIdentifierMerges(t *testing.T) {
	mode := directory.Auto
	meta := catalog.Metadata{Title: "Catalog Title", Author: "Catalog Author", Isbn: "1234567890", Issn: "4321-4321"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	h := PatronRequestApiHandler{}
	h.SetLookupAdapterFactory(factory)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title: "Patron Title", // no SupplierUniqueRecordId → Auto resolves to Merge
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifier:     "0987654321",
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
				},
			},
		},
	}
	err := h.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Patron Title", req.BibliographicInfo.Title)                                               // preserved (Merge)
	assert.Equal(t, "Catalog Author", req.BibliographicInfo.Author)                                            // filled in (was empty)
	assert.Equal(t, "0987654321", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier)    // kept
	assert.Equal(t, "ISBN", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text) // kept
	assert.Equal(t, "4321-4321", req.BibliographicInfo.BibliographicItemId[1].BibliographicItemIdentifier)     // added (not present)
	assert.Equal(t, "ISSN", req.BibliographicInfo.BibliographicItemId[1].BibliographicItemIdentifierCode.Text) // added (not present)
}

// --- PutPatronRequestsId tests ---

// illRepoNoTx returns pgx.ErrNoRows for GetIllTransactionByRequesterRequestId,
// meaning no ILL transaction has been sent for the given patron request yet.
// PrRepoUpdateCapture captures the params passed to UpdatePatronRequest and returns success.
type PrRepoUpdateCapture struct {
	PrRepoError
	lastUpdateParams *pr_db.UpdatePatronRequestParams
}

func (r *PrRepoUpdateCapture) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	if id == "3" {
		return pr_db.PatronRequest{ID: id, State: prservice.BorrowerStateNeedsReview, Side: prservice.SideBorrowing, RequesterSymbol: pgtype.Text{String: symbol, Valid: true}, InternalNote: pgtype.Text{String: "original note", Valid: true}, Patron: pgtype.Text{String: "original patron", Valid: true}}, nil
	}
	return r.PrRepoError.GetPatronRequestById(ctx, id)
}

func (r *PrRepoUpdateCapture) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	paramsCopy := params
	r.lastUpdateParams = &paramsCopy
	return pr_db.PatronRequest(paramsCopy), nil
}

// PrRepoUpdateCapturePreset embeds PrRepoUpdateCapture but returns a PR with a preset CreatedAt for id "3".
type PrRepoUpdateCapturePreset struct {
	PrRepoUpdateCapture
	presetCreatedAt pgtype.Timestamp
}

func (r *PrRepoUpdateCapturePreset) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	pr, err := r.PrRepoUpdateCapture.GetPatronRequestById(ctx, id)
	if err == nil {
		pr.CreatedAt = r.presetCreatedAt
	}
	return pr, err
}

// PrRepoWrongOwner returns a PR whose RequesterSymbol does not match the requesting symbol.
type PrRepoWrongOwner struct {
	PrRepoError
}

// PrRepoLendingSide returns a lending-side PR for id "3".
type PrRepoLendingSide struct {
	PrRepoError
}

// PrRepoBranchOwner returns a borrowing-side PR for id "3" whose RequesterSymbol
// is "ISIL:S1" — the branch symbol that MockIllRepositorySuccess returns for any
// peer, allowing a test to reach the requesterSymbol mismatch check.
type PrRepoBranchOwner struct {
	PrRepoError
}

func (r *PrRepoBranchOwner) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	if id == "3" {
		return pr_db.PatronRequest{
			ID:              id,
			State:           prservice.BorrowerStateNew,
			Side:            prservice.SideBorrowing,
			RequesterSymbol: pgtype.Text{String: "ISIL:S1", Valid: true},
		}, nil
	}
	return r.PrRepoError.GetPatronRequestById(ctx, id)
}

func (r *PrRepoWrongOwner) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	if id == "3" {
		return pr_db.PatronRequest{
			ID:              id,
			State:           prservice.BorrowerStateNew,
			Side:            prservice.SideBorrowing,
			RequesterSymbol: pgtype.Text{String: "ISIL:OTHER", Valid: true},
		}, nil
	}
	return r.PrRepoError.GetPatronRequestById(ctx, id)
}

func (r *PrRepoLendingSide) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	if id == "3" {
		return pr_db.PatronRequest{
			ID:             id,
			State:          prservice.BorrowerStateNew,
			Side:           prservice.SideLending,
			SupplierSymbol: pgtype.Text{String: symbol, Valid: true},
		}, nil
	}
	return r.PrRepoError.GetPatronRequestById(ctx, id)
}

func putBody(t *testing.T, id string, illReq iso18626.Request) *bytes.Buffer {
	t.Helper()
	toUpdate := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &symbol,
		IllRequest:      illReq,
	}
	jsonBytes, err := json.Marshal(toUpdate)
	assert.NoError(t, err)
	return bytes.NewBuffer(jsonBytes)
}

func TestPutPatronRequestsIdMissingBody(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", nil)
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "body is required")
}

func TestPutPatronRequestsIdInvalidJson(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBufferString("{bad json"))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPutPatronRequestsIdEmptySymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	body := proapi.CreatePatronRequest{IllRequest: validIllRequest()}
	jsonBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "symbol must be specified")
}

func TestPutPatronRequestsIdMissingId(t *testing.T) {
	repo := new(PrRepoUpdateCapture)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	body := proapi.CreatePatronRequest{RequesterSymbol: &symbol, IllRequest: validIllRequest()}
	jsonBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.lastUpdateParams) {
		assert.Equal(t, "3", repo.lastUpdateParams.ID)
	}
}

func TestPutPatronRequestsIdIdMismatch(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	otherId := "other-id"
	body := proapi.CreatePatronRequest{Id: &otherId, RequesterSymbol: &symbol, IllRequest: validIllRequest()}
	jsonBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "patron request id does not match")
}

func TestPutPatronRequestsIdNotEditable(t *testing.T) {
	// id "3" returns a NEW state PR, which is not editable.
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "3", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "patron request is not editable in state")
}

func TestPutPatronRequestsIdNotFound(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "2", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "2", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestPutPatronRequestsIdPrRepoError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "1", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "1", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPutPatronRequestsIdWrongOwner(t *testing.T) {
	// WithIllRepo on the resolver is required so that IsOwnerOf can look up branch symbols
	// without erroring; the mock returns no branch symbols for "ISIL:OTHER", producing 404.
	tenantResolver := tenant.NewResolver().WithIllRepo(new(mocks.MockIllRepositorySuccess))
	handler := NewPrApiHandler(new(PrRepoWrongOwner), mockEventBus, mockEventRepo, tenantResolver, nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "3", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestPutPatronRequestsIdUpdateError(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "5", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "5", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "DB error")
}

func TestPutPatronRequestsIdNotBorrowingSide(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoLendingSide), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "3", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "only borrower-side patron requests can be updated")
}

func TestPutPatronRequestsIdRequesterSymbolMismatch(t *testing.T) {
	// "ISIL:DIFFERENT" is the body/request symbol; MockIllRepositorySuccess returns
	// "ISIL:S1" as a branch of that symbol, so ownership passes. The existing PR
	// stores "ISIL:S1" directly, which differs from the body symbol → mismatch error.
	tenantResolver := tenant.NewResolver().WithIllRepo(new(mocks.MockIllRepositorySuccess))
	handler := NewPrApiHandler(new(PrRepoBranchOwner), mockEventBus, mockEventRepo, tenantResolver, nil, 10)
	differentSymbol := "ISIL:DIFFERENT"
	id := "3"
	body := proapi.CreatePatronRequest{Id: &id, RequesterSymbol: &differentSymbol, IllRequest: validIllRequest()}
	jsonBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "requesterSymbol does not match existing patron request")
}

func TestPutPatronRequestsIdPreservesCreatedAt(t *testing.T) {
	originalCreatedAt := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	repo := &PrRepoUpdateCapturePreset{
		presetCreatedAt: pgtype.Timestamp{Valid: true, Time: originalCreatedAt},
	}
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	req, _ := http.NewRequest("PUT", "/", putBody(t, "3", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.lastUpdateParams) {
		assert.Equal(t, originalCreatedAt, repo.lastUpdateParams.IllRequest.Header.Timestamp.Time)
	}
}

func TestPutPatronRequestsIdOK(t *testing.T) {
	repo := new(PrRepoUpdateCapture)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	id := "3"
	patron := "user-1"
	note := "staff note"
	toUpdate := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &symbol,
		IllRequest:      validIllRequest(),
		Patron:          &patron,
		InternalNote:    &note,
	}
	jsonBytes, _ := json.Marshal(toUpdate)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.lastUpdateParams) {
		assert.Equal(t, validIllRequest().BibliographicInfo.Title, repo.lastUpdateParams.IllRequest.BibliographicInfo.Title)
		assert.True(t, repo.lastUpdateParams.Patron.Valid)
		assert.Equal(t, patron, repo.lastUpdateParams.Patron.String)
		assert.True(t, repo.lastUpdateParams.InternalNote.Valid)
		assert.Equal(t, note, repo.lastUpdateParams.InternalNote.String)
		assert.Equal(t, "returnables", repo.lastUpdateParams.StateModel)
	}
	var response proapi.PatronRequest
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, id, response.Id)
}

func TestPutPatronRequestsIdNotePatronAbsent(t *testing.T) {
	repo := new(PrRepoUpdateCapture)
	handler := NewPrApiHandler(repo, mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	id := "3"
	toUpdate := proapi.CreatePatronRequest{
		Id:              &id,
		RequesterSymbol: &symbol,
		IllRequest:      validIllRequest(),
	}
	jsonBytes, _ := json.Marshal(toUpdate)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "3", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusOK, rr.Code)
	if assert.NotNil(t, repo.lastUpdateParams) {
		assert.Equal(t, validIllRequest().BibliographicInfo.Title, repo.lastUpdateParams.IllRequest.BibliographicInfo.Title)
		assert.True(t, repo.lastUpdateParams.Patron.Valid)
		assert.Equal(t, "original patron", repo.lastUpdateParams.Patron.String)
		assert.Equal(t, "original note", repo.lastUpdateParams.InternalNote.String)
	}
	var response proapi.PatronRequest
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, id, response.Id)
}

func TestPutPatronRequestsIdEmptyIllRequest(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	id := "5"
	body := proapi.CreatePatronRequest{Id: &id, RequesterSymbol: &symbol}
	// IllRequest is zero-valued, which parseAndValidateIllRequest rejects as errInvalidPatronRequest
	jsonBytes, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", "/", bytes.NewBuffer(jsonBytes))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "5", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "illRequest must not be empty")
}

func TestPutPatronRequestsIdInvalidBrokerSymbol(t *testing.T) {
	handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
	previousBrokerSymbol := brokerSymbol
	brokerSymbol = "BROKER"
	defer func() {
		brokerSymbol = previousBrokerSymbol
	}()
	req, _ := http.NewRequest("PUT", "/", putBody(t, "5", validIllRequest()))
	rr := httptest.NewRecorder()
	handler.PutPatronRequestsId(rr, req, "5", proapi.PutPatronRequestsIdParams{})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid BROKER_SYMBOL")
}
