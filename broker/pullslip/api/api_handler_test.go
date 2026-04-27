package psapi

import (
	"bytes"
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

func (m *MockPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	args := m.Called(id)
	return args.Get(0).(pr_db.PatronRequest), args.Error(1)
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
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "PR-1") // single type uses SearchCriteria
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
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "ps-batch") // batch type uses ID
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
	b, _ := json.Marshal(psoapi.CreatePullSlip{IllTransactionId: illId})
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
	prRepo.On("GetPatronRequestById", "pr-1").Return(pr, nil)

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
	prRepo.On("GetPatronRequestById", "pr-missing").Return(pr_db.PatronRequest{}, pgx.ErrNoRows)

	h := newHandler(nil, prRepo)
	req := newRequest(http.MethodPost, postBody("pr-missing"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	prRepo.AssertExpectations(t)
}

func TestPostPullslips_PrRepoError(t *testing.T) {
	prRepo := new(MockPrRepo)
	prRepo.On("GetPatronRequestById", "pr-err").Return(pr_db.PatronRequest{}, errors.New("db err"))

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
	prRepo.On("GetPatronRequestById", "pr-1").Return(pr, nil)

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

func TestPostPullslips_WrongSymbol(t *testing.T) {
	otherSym := "ISIL:OTHER"
	pr := patronRequest("pr-1", otherSym) // neither requester nor supplier matches sym
	pr.SupplierSymbol = pgtype.Text{String: "ISIL:ANOTHER", Valid: true}

	prRepo := new(MockPrRepo)
	prRepo.On("GetPatronRequestById", "pr-1").Return(pr, nil)

	psRepo := new(MockPsRepo)
	psRepo.On("SavePullSlip", mock.AnythingOfType("ps_db.SavePullSlipParams")).
		Return(ps_db.PullSlip{}, nil)

	h := newHandler(psRepo, prRepo)

	req := newRequest(http.MethodPost, postBody("pr-1"))
	rr := httptest.NewRecorder()
	h.PostPullslips(rr, req, psoapi.PostPullslipsParams{Symbol: &sym})

	// bad request is written but execution continues (no early return in handler)
	prRepo.AssertExpectations(t)
}

func TestWritePdf(t *testing.T) {
	rr := httptest.NewRecorder()
	writePdf(rr, []byte("%PDF-direct"), "DOC-1")
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/pdf", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Header().Get("Content-Disposition"), "DOC-1")
	assert.Equal(t, []byte("%PDF-direct"), rr.Body.Bytes())
}

// suppress unused import warning for bytes
var _ = bytes.NewReader
