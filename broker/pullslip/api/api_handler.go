package psapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	prapi "github.com/indexdata/crosslink/broker/patron_request/api"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	ps_db "github.com/indexdata/crosslink/broker/pullslip/db"
	psoapi "github.com/indexdata/crosslink/broker/pullslip/oapi"
	psservice "github.com/indexdata/crosslink/broker/pullslip/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const MAX_RECORDS_PER_PDF = 100

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
		pdfService:     psservice.NewPdfService(prRepo),
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
		Type:           psoapi.PullSlipType(ps.Type),
		Owner:          ps.Owner,
		SearchCriteria: ps.SearchCriteria,
		PdfLink:        new(api.Link(r, api.Path("pullslips", id, "pdf"), nil)),
	}
	if ps.GeneratedAt.Valid {
		resp.GeneratedAt = &ps.GeneratedAt.Time
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
	writePdf(w, ps.PdfBytes)
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
	cql := ""
	pullSlipType := ps_db.PullSlipTypeBatch
	if create.IllTransactionIds != nil && len(*create.IllTransactionIds) > 0 {
		if len(*create.IllTransactionIds) == 1 {
			pullSlipType = ps_db.PullSlipTypeSingle
		}
		query, err := cqlbuilder.NewQuery().Search("id").Rel("any").Term(strings.Join(*create.IllTransactionIds, " ")).Build()
		if err != nil {
			api.AddBadRequestError(ctx, w, err)
			return
		}
		cql = query.String()
	} else if create.Cql != nil {
		cql = *create.Cql
	}
	if cql == "" {
		api.AddBadRequestError(ctx, w, errors.New("search criteria is empty"))
		return
	}
	qb, err := cqlbuilder.NewQueryFromString(cql)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	var side pr_db.PatronRequestSide
	qb, err = prapi.AddOwnerRestriction(qb, symbol, side)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	cqlQuery, err := qb.Build()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}

	pdf, err := p.getPdfByte(ctx, w, cqlQuery.String())
	if err != nil {
		return // http response already added
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
		Type:           pullSlipType,
		Owner:          symbol,
		SearchCriteria: cqlQuery.String(),
		PdfBytes:       pdf,
	})
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Location", api.Link(r, api.Path("pullslips", psId, "pdf"), nil))
	writePdf(w, pdf)
}

func (p PullSlipApiHandler) PostPullslipsIdRegenerate(w http.ResponseWriter, r *http.Request, id string, params psoapi.PostPullslipsIdRegenerateParams) {
	logParams := map[string]string{"method": "PostPullslipsIdRegenerate", "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	ps := p.getPullSlip(ctx, w, r, id, psoapi.GetPullslipsIdParams(params), logParams)
	if ps == nil {
		return
	}

	pdf, err := p.getPdfByte(ctx, w, ps.SearchCriteria)
	if err != nil {
		return // http response already added
	}

	ps.PdfBytes = pdf
	ps.GeneratedAt = pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
	_, err = p.psRepo.SavePullSlip(ctx, ps_db.SavePullSlipParams(*ps))
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Location", api.Link(r, api.Path("pullslips", ps.ID, "pdf"), nil))
	writePdf(w, pdf)
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

func (p PullSlipApiHandler) getPdfByte(ctx common.ExtendedContext, w http.ResponseWriter, cql string) ([]byte, error) {
	prs, _, err := p.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: MAX_RECORDS_PER_PDF, Offset: 0}, &cql)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.AddNotFoundError(w)
			return []byte{}, err
		}
		api.AddInternalError(ctx, w, err)
		return []byte{}, err
	}

	if len(prs) == 0 {
		api.AddBadRequestError(ctx, w, errors.New("no patron requests found"))
		return []byte{}, errors.New("no patron requests found")
	}

	pdf, err := p.pdfService.GeneratePdfPullSlipForPrs(ctx, prs)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return []byte{}, err
	}
	return pdf, nil
}

func writePdf(w http.ResponseWriter, bytes []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="pull-slips.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes) // #nosec G705 -- content is a generated PDF binary; Content-Type is set to application/pdf
}
