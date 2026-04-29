package psapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	ps_db "github.com/indexdata/crosslink/broker/pullslip/db"
	psoapi "github.com/indexdata/crosslink/broker/pullslip/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ── mocks ────────────────────────────────────────────────────────────────────

type MockPsRepo struct {
	mock.Mock
	ps_db.PsRepo
}

func (m *MockPsRepo) GetPullSlipByIdAndOwner(ctx common.ExtendedContext, id string, owner string) (ps_db.PullSlip, error) {
	args := m.Called(id, owner)
	return args.Get(0).(ps_db.PullSlip), args.Error(1)
}

func (m *MockPsRepo) SavePullSlip(ctx common.ExtendedContext, params ps_db.SavePullSlipParams) (ps_db.PullSlip, error) {
	args := m.Called(params)
	return args.Get(0).(ps_db.PullSlip), args.Error(1)
}

type MockPrRepo struct {
	mock.Mock
	pr_db.PrRepo
}

func (m *MockPrRepo) ListPatronRequests(ctx common.ExtendedContext, params pr_db.ListPatronRequestsParams, cql *string) ([]pr_db.PatronRequest, int64, error) {
	args := m.Called(*cql)
	return args.Get(0).([]pr_db.PatronRequest), args.Get(1).(int64), args.Error(2)
}

func (m *MockPrRepo) GetNotificationsByPrId(ctx common.ExtendedContext, params pr_db.GetNotificationsByPrIdParams) ([]pr_db.Notification, int64, error) {
	args := m.Called(params.PrID, params.Kind)
	return args.Get(0).([]pr_db.Notification), args.Get(1).(int64), args.Error(2)
}

// ── helpers ───────────────────────────────────────────────────────────────────

var sym = "ISIL:TEST"

func newHandler(psRepo ps_db.PsRepo, prRepo pr_db.PrRepo) PullSlipApiHandler {
	return NewPsApiHandler(psRepo, prRepo, *tenant.NewResolver())
}

func newRequest(method, body string) *http.Request {
	if body != "" {
		return httptest.NewRequest(method, "/pullslips", strings.NewReader(body))
	}
	return httptest.NewRequest(method, "/pullslips", nil)
}

func pullSlipFixture(id string) ps_db.PullSlip {
	return ps_db.PullSlip{
		ID:             id,
		Type:           ps_db.PullSlipTypeSingle,
		Owner:          sym,
		SearchCriteria: "PR-1",
		PdfBytes:       []byte("%PDF-fixture"),
	}
}

// ── GetPullslipsId ────────────────────────────────────────────────────────────

func TestGetPullslipsId_OK(t *testing.T) {
	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-1", sym).Return(pullSlipFixture("ps-1"), nil)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsId(rr, req, "ps-1", psoapi.GetPullslipsIdParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	var resp psoapi.PullSlip
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ps-1", resp.Id)
	psRepo.AssertExpectations(t)
}

func TestGetPullslipsId_NotFound(t *testing.T) {
	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "missing", sym).Return(ps_db.PullSlip{}, pgx.ErrNoRows)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsId(rr, req, "missing", psoapi.GetPullslipsIdParams{Symbol: &sym})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	psRepo.AssertExpectations(t)
}

func TestGetPullslipsId_InternalError(t *testing.T) {
	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-err", sym).Return(ps_db.PullSlip{}, errors.New("db failure"))

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsId(rr, req, "ps-err", psoapi.GetPullslipsIdParams{Symbol: &sym})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	psRepo.AssertExpectations(t)
}

func TestGetPullslipsId_WithGeneratedAt(t *testing.T) {
	slip := pullSlipFixture("ps-gen")
	slip.GeneratedAt = pgtype.Timestamp{Time: slip.CreatedAt.Time, Valid: true}

	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-gen", sym).Return(slip, nil)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsId(rr, req, "ps-gen", psoapi.GetPullslipsIdParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp psoapi.PullSlip
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.NotNil(t, resp.GeneratedAt)
	psRepo.AssertExpectations(t)
}

// ── GetPullslipsIdPdf ─────────────────────────────────────────────────────────

func TestGetPullslipsIdPdf_OK(t *testing.T) {
	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-1", sym).Return(pullSlipFixture("ps-1"), nil)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsIdPdf(rr, req, "ps-1", psoapi.GetPullslipsIdPdfParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "pull-slips") // single type uses SearchCriteria
	assert.Equal(t, []byte("%PDF-fixture"), rr.Body.Bytes())
	psRepo.AssertExpectations(t)
}

func TestGetPullslipsIdPdf_BatchType(t *testing.T) {
	slip := pullSlipFixture("ps-batch")
	slip.Type = ps_db.PullSlipTypeBatch

	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-batch", sym).Return(slip, nil)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsIdPdf(rr, req, "ps-batch", psoapi.GetPullslipsIdPdfParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "pull-slips") // batch type uses ID
	psRepo.AssertExpectations(t)
}

func TestGetPullslipsIdPdf_NotFound(t *testing.T) {
	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "missing", sym).Return(ps_db.PullSlip{}, pgx.ErrNoRows)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetPullslipsIdPdf(rr, req, "missing", psoapi.GetPullslipsIdPdfParams{Symbol: &sym})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	psRepo.AssertExpectations(t)
}

// ── PostPullslips ─────────────────────────────────────────────────────────────

func postBody(illId string) string {
	ids := []string{illId}
	b, _ := json.Marshal(psoapi.CreatePullSlip{IllTransactionIds: &ids})
	return string(b)
}

func patronRequest(id, requesterSym string) pr_db.PatronRequest {
	return pr_db.PatronRequest{
		ID:              id,
		RequesterSymbol: pgtype.Text{String: requesterSym, Valid: true},
		RequesterReqID:  pgtype.Text{String: "REQ-" + id, Valid: true},
	}
}

func TestPostPullslips_OK(t *testing.T) {
	pr := patronRequest("pr-1", sym)
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", "id any pr-1 and (side = lending and supplier_symbol_exact = ISIL:TEST) or (side = borrowing and requester_symbol_exact = ISIL:TEST)").Return([]pr_db.PatronRequest{pr}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindNote)).Return([]pr_db.Notification{{Note: pgtype.Text{String: "Be careful with item"}}}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindCondition)).Return([]pr_db.Notification{{Condition: pgtype.Text{String: "Library use only"}}}, int64(1), nil)

	psRepo := new(MockPsRepo)
	psRepo.On("SavePullSlip", mock.AnythingOfType("ps_db.SavePullSlipParams")).
		Return(ps_db.PullSlip{}, nil)

	h := newHandler(psRepo, prRepo)

	req := newRequest(http.MethodPost, postBody("pr-1"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	assert.NotEmpty(t, rr.Header().Get("Location"))
	prRepo.AssertExpectations(t)
	psRepo.AssertExpectations(t)
}

func TestPostPullslips_NilBody(t *testing.T) {
	h := newHandler(nil, nil)
	req := newRequest(http.MethodPost, "")
	req.Body = nil
	rr := httptest.NewRecorder()
	// Should not panic; bad request is written
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})
	// body is nil → JSON decode will fail → bad request
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostPullslips_InvalidJSON(t *testing.T) {
	h := newHandler(nil, nil)
	req := newRequest(http.MethodPost, "not-json")
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostPullslips_PrNotFound(t *testing.T) {
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", "id any pr-missing and (side = lending and supplier_symbol_exact = ISIL:TEST) or (side = borrowing and requester_symbol_exact = ISIL:TEST)").Return([]pr_db.PatronRequest{}, int64(1), pgx.ErrNoRows)

	h := newHandler(nil, prRepo)
	req := newRequest(http.MethodPost, postBody("pr-missing"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	prRepo.AssertExpectations(t)
}

func TestPostPullslips_PrRepoError(t *testing.T) {
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", "id any pr-err and (side = lending and supplier_symbol_exact = ISIL:TEST) or (side = borrowing and requester_symbol_exact = ISIL:TEST)").Return([]pr_db.PatronRequest{}, int64(1), errors.New("db err"))

	h := newHandler(nil, prRepo)
	req := newRequest(http.MethodPost, postBody("pr-err"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	prRepo.AssertExpectations(t)
}

func TestPostPullslips_SaveError(t *testing.T) {
	pr := patronRequest("pr-1", sym)

	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", "id any pr-1 and (side = lending and supplier_symbol_exact = ISIL:TEST) or (side = borrowing and requester_symbol_exact = ISIL:TEST)").Return([]pr_db.PatronRequest{pr}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindNote)).Return([]pr_db.Notification{{Note: pgtype.Text{String: "Be careful with item"}}}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindCondition)).Return([]pr_db.Notification{{Condition: pgtype.Text{String: "Library use only"}}}, int64(1), nil)

	psRepo := new(MockPsRepo)
	psRepo.On("SavePullSlip", mock.AnythingOfType("ps_db.SavePullSlipParams")).
		Return(ps_db.PullSlip{}, errors.New("save failed"))

	h := newHandler(psRepo, prRepo)

	req := newRequest(http.MethodPost, postBody("pr-1"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	prRepo.AssertExpectations(t)
	psRepo.AssertExpectations(t)
}

func TestPostPullslips_EmptyBody(t *testing.T) {
	h := newHandler(nil, nil)
	req := newRequest(http.MethodPost, `{}`)
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})
	// both IllTransactionIds and Cql are nil → search criteria is empty
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostPullslips_EmptyIdList(t *testing.T) {
	h := newHandler(nil, nil)
	ids := []string{}
	b, _ := json.Marshal(psoapi.CreatePullSlip{IllTransactionIds: &ids})
	req := newRequest(http.MethodPost, string(b))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func cqlBody(cql string) string {
	b, _ := json.Marshal(psoapi.CreatePullSlip{Cql: &cql})
	return string(b)
}

func TestPostPullslips_CqlBased_OK(t *testing.T) {
	pr := patronRequest("pr-cql", sym)
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", mock.AnythingOfType("string")).Return([]pr_db.PatronRequest{pr}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-cql", string(pr_db.NotificationKindNote)).Return([]pr_db.Notification{}, int64(0), nil)
	prRepo.On("GetNotificationsByPrId", "pr-cql", string(pr_db.NotificationKindCondition)).Return([]pr_db.Notification{}, int64(0), nil)

	psRepo := new(MockPsRepo)
	psRepo.On("SavePullSlip", mock.AnythingOfType("ps_db.SavePullSlipParams")).Return(ps_db.PullSlip{}, nil)

	h := newHandler(psRepo, prRepo)
	req := newRequest(http.MethodPost, cqlBody("state = WILL_SUPPLY"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	prRepo.AssertExpectations(t)
	psRepo.AssertExpectations(t)
}

func TestPostPullslips_EmptyPrList(t *testing.T) {
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", mock.AnythingOfType("string")).Return([]pr_db.PatronRequest{}, int64(0), nil)

	h := newHandler(nil, prRepo)
	req := newRequest(http.MethodPost, postBody("pr-none"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	prRepo.AssertExpectations(t)
}

// ── PostPullslipsIdRegenerate ─────────────────────────────────────────────────

func TestPostPullslipsIdRegenerate_OK(t *testing.T) {
	slip := pullSlipFixture("ps-1")
	slip.SearchCriteria = "id any pr-1 and (side = lending and supplier_symbol_exact = ISIL:TEST) or (side = borrowing and requester_symbol_exact = ISIL:TEST)"

	pr := patronRequest("pr-1", sym)
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", slip.SearchCriteria).Return([]pr_db.PatronRequest{pr}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindNote)).Return([]pr_db.Notification{}, int64(0), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindCondition)).Return([]pr_db.Notification{}, int64(0), nil)

	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-1", sym).Return(slip, nil)
	psRepo.On("SavePullSlip", mock.AnythingOfType("ps_db.SavePullSlipParams")).Return(slip, nil)

	h := newHandler(psRepo, prRepo)
	req := newRequest(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostPullslipsIdRegenerate(rr, req, "ps-1", psoapi.PostPullslipsIdRegenerateParams{Symbol: &sym})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	assert.NotEmpty(t, rr.Header().Get("Location"))
	prRepo.AssertExpectations(t)
	psRepo.AssertExpectations(t)
}

func TestPostPullslipsIdRegenerate_NotFound(t *testing.T) {
	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "missing", sym).Return(ps_db.PullSlip{}, pgx.ErrNoRows)

	h := newHandler(psRepo, nil)
	req := newRequest(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostPullslipsIdRegenerate(rr, req, "missing", psoapi.PostPullslipsIdRegenerateParams{Symbol: &sym})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	psRepo.AssertExpectations(t)
}

func TestPostPullslipsIdRegenerate_PrRepoError(t *testing.T) {
	slip := pullSlipFixture("ps-err")
	slip.SearchCriteria = "state = WILL_SUPPLY"

	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-err", sym).Return(slip, nil)

	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", slip.SearchCriteria).Return([]pr_db.PatronRequest{}, int64(0), errors.New("db err"))

	h := newHandler(psRepo, prRepo)
	req := newRequest(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostPullslipsIdRegenerate(rr, req, "ps-err", psoapi.PostPullslipsIdRegenerateParams{Symbol: &sym})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	psRepo.AssertExpectations(t)
	prRepo.AssertExpectations(t)
}

func TestPostPullslipsIdRegenerate_SaveError(t *testing.T) {
	slip := pullSlipFixture("ps-saveerr")
	slip.SearchCriteria = "id any pr-1 and (side = lending and supplier_symbol_exact = ISIL:TEST) or (side = borrowing and requester_symbol_exact = ISIL:TEST)"

	pr := patronRequest("pr-1", sym)
	prRepo := new(MockPrRepo)
	prRepo.On("ListPatronRequests", slip.SearchCriteria).Return([]pr_db.PatronRequest{pr}, int64(1), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindNote)).Return([]pr_db.Notification{}, int64(0), nil)
	prRepo.On("GetNotificationsByPrId", "pr-1", string(pr_db.NotificationKindCondition)).Return([]pr_db.Notification{}, int64(0), nil)

	psRepo := new(MockPsRepo)
	psRepo.On("GetPullSlipByIdAndOwner", "ps-saveerr", sym).Return(slip, nil)
	psRepo.On("SavePullSlip", mock.AnythingOfType("ps_db.SavePullSlipParams")).Return(ps_db.PullSlip{}, errors.New("save failed"))

	h := newHandler(psRepo, prRepo)
	req := newRequest(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostPullslipsIdRegenerate(rr, req, "ps-saveerr", psoapi.PostPullslipsIdRegenerateParams{Symbol: &sym})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	psRepo.AssertExpectations(t)
	prRepo.AssertExpectations(t)
}

func TestWritePdf(t *testing.T) {
	rr := httptest.NewRecorder()
	writePdf(rr, []byte("%PDF-direct"))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "pull-slips")
	assert.Equal(t, []byte("%PDF-direct"), rr.Body.Bytes())
}
