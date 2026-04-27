package psapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	ps_db "github.com/indexdata/crosslink/broker/pullslip/db"
	psoapi "github.com/indexdata/crosslink/broker/pullslip/oapi"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type PullSlipApiHandler struct {
	psRepo         ps_db.PsRepo
	prRepo         pr_db.PrRepo
	pdfService     psservice.PdfService
	tenantResolver tenant.TenantResolver
}

func NewPsApiHandler(psRepo ps_db.PsRepo, prRepo pr_db.PrRepo, tenantResolver tenant.TenantResolver) PullSlipApiHandler {
	return PullSlipApiHandler{
		psRepo:         psRepo,
		prRepo:         prRepo,
		tenantResolver: tenantResolver,
		pdfService:     psservice.PdfService{},
	}
}

func (p PullSlipApiHandler) GetPullslipsId(w http.ResponseWriter, r *http.Request, id string, params psoapi.GetPullslipsIdParams) {
	logParams := map[string]string{"method": "GetPullslipsId", "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	ps := p.getPullSlip(ctx, w, r, id, params, logParams)
	if ps == nil {
		return
	}
	resp := psoapi.PullSlip{
		Id:             ps.ID,
		CreatedAt:      ps.CreatedAt.Time,
		GeneratedAt:    &ps.GeneratedAt.Time,
		Type:           psoapi.PullSlipType(ps.Type),
		Owner:          ps.Owner,
		SearchCriteria: ps.SearchCriteria,
		PdfLink:        new(api.Link(r, api.Path("pullslips", id, "pdf"), nil)),
	}
	api.WriteJsonResponse(w, resp)
}

func (p PullSlipApiHandler) GetPullslipsIdPdf(w http.ResponseWriter, r *http.Request, id string, params psoapi.GetPullslipsIdPdfParams) {
	logParams := map[string]string{"method": "GetPullslipsIdPdf", "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	ps := p.getPullSlip(ctx, w, r, id, psoapi.GetPullslipsIdParams(params), logParams)
	if ps == nil {
		return
	}
	pdfId := ps.ID
	if ps.Type == ps_db.PullSlipTypeSingle {
		pdfId = ps.SearchCriteria
	}
	writePdf(w, ps.PdfBytes, pdfId)
}

func (p PullSlipApiHandler) PostPullslips(w http.ResponseWriter, r *http.Request, params psoapi.PostPullslipsParams) {
	logParams := map[string]string{"method": "PostPullslips"}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	if r.Body == nil || r.Body == http.NoBody {
		api.AddBadRequestError(ctx, w, errors.New("missing body"))
		return
	}
	var create psoapi.CreatePullSlip
	err := json.NewDecoder(r.Body).Decode(&create)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}

	tenant, err := p.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	pr, err := p.prRepo.GetPatronRequestById(ctx, create.IllTransactionId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.AddNotFoundError(w)
			return
		}
		api.AddInternalError(ctx, w, err)
		return
	}

	if pr.RequesterSymbol.String != symbol && pr.SupplierSymbol.String != symbol {
		api.AddBadRequestError(ctx, w, fmt.Errorf("patron request does not have the correct symbol"))
	}

	pdf, err := p.pdfService.GeneratePdfPullSlip(pr)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	psId := uuid.NewString()
	_, err = p.psRepo.SavePullSlip(ctx, ps_db.SavePullSlipParams{
		ID: psId,
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		GeneratedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Type:           ps_db.PullSlipTypeSingle,
		Owner:          symbol,
		SearchCriteria: create.IllTransactionId,
		PdfBytes:       pdf,
	})
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Location", api.Link(r, api.Path("pullslips", psId, "pdf"), nil))
	writePdf(w, pdf, pr.RequesterReqID.String)
}

func (p PullSlipApiHandler) getPullSlip(ctx common.ExtendedContext, w http.ResponseWriter, r *http.Request, id string, params psoapi.GetPullslipsIdParams, logParams map[string]string) *ps_db.PullSlip {
	tenant, err := p.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return nil
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return nil
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	ps, err := p.psRepo.GetPullSlipByIdAndOwner(ctx, id, symbol)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.AddNotFoundError(w)
			return nil
		}
		api.AddInternalError(ctx, w, err)
		return nil
	}
	return &ps
}

func writePdf(w http.ResponseWriter, bytes []byte, id string) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pull-slip-%s.pdf"`, id))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes)
}
