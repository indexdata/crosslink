package pr_db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PrRepo interface {
	repo.Transactional[PrRepo]
	GetPatronRequestById(ctx common.ExtendedContext, id string) (PatronRequest, error)
	GetPatronRequestSearchView(ctx common.ExtendedContext, id string) (PatronRequestSearchView, error)
	GetPatronRequestByIdForUpdate(ctx common.ExtendedContext, id string) (PatronRequest, error)
	GetPatronRequestByIdAndSide(ctx common.ExtendedContext, id string, side PatronRequestSide) (PatronRequest, error)
	ListPatronRequests(ctx common.ExtendedContext, args ListPatronRequestsParams, pgcql pgcql.Query) ([]PatronRequest, int64, error)
	ListPatronRequestsSearchView(ctx common.ExtendedContext, args ListPatronRequestsParams, pgcql pgcql.Query) ([]PatronRequestSearchView, int64, error)
	GetPatronRequestsFacets(ctx common.ExtendedContext, facetFields []string, pgcql pgcql.Query) ([]Facet, error)
	UpdatePatronRequest(ctx common.ExtendedContext, params UpdatePatronRequestParams) (PatronRequest, error)
	UpdatePatronRequestInternalNote(ctx common.ExtendedContext, id string, internalNote pgtype.Text) error
	CreatePatronRequest(ctx common.ExtendedContext, params CreatePatronRequestParams) (PatronRequest, error)
	DeletePatronRequest(ctx common.ExtendedContext, id string) error
	GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx common.ExtendedContext, supplierSymbol string, requesterReId string) (PatronRequest, error)
	GetNextHrid(ctx common.ExtendedContext, prefix string) (string, error)
	SaveItem(ctx common.ExtendedContext, params SaveItemParams) (Item, error)
	GetItemById(ctx common.ExtendedContext, id string) (Item, error)
	GetItemsByPrId(ctx common.ExtendedContext, prId string) ([]Item, error)
	SaveNotification(ctx common.ExtendedContext, params SaveNotificationParams) (Notification, error)
	GetNotificationById(ctx common.ExtendedContext, id string) (Notification, error)
	GetNotificationsByPrId(ctx common.ExtendedContext, params GetNotificationsByPrIdParams) ([]Notification, int64, error)
	MarkConditionNotificationsReceipt(ctx common.ExtendedContext, params MarkConditionNotificationsReceiptParams) error
	DeleteNotificationById(ctx common.ExtendedContext, id string) error
	DeleteItemById(ctx common.ExtendedContext, id string) error

	SaveTemplate(ctx common.ExtendedContext, params SaveTemplateParams) (Template, error)
	GetTemplateByIdAndOwner(ctx common.ExtendedContext, id string, owner string) (Template, error)
	GetTemplatesByOwner(ctx common.ExtendedContext, params GetTemplatesByOwnerParams) ([]Template, int64, error)
	GetTemplateByPurposeAudienceLabelAndOwner(ctx common.ExtendedContext, params GetTemplateByPurposeAudienceLabelAndOwnerParams) (Template, error)
	DeleteTemplateByIdAndOwner(ctx common.ExtendedContext, id string, owner string) error
}

var ErrUnsupportedFacet = errors.New("unsupported facet field")

type Facet struct {
	Field  string
	Values []FacetValue
}

type FacetValue struct {
	Value string
	Count int64
}

type PgPrRepo struct {
	repo.PgBaseRepo[PrRepo]
	queries        Queries
	explainAnalyze bool
}

// delegate transaction handling to Base
func (r *PgPrRepo) WithTxFunc(ctx common.ExtendedContext, fn func(PrRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

func CreatePrRepo(dbPool *pgxpool.Pool, explainAnalyze bool) PrRepo {
	prRepo := new(PgPrRepo)
	prRepo.Pool = dbPool
	prRepo.explainAnalyze = explainAnalyze
	return prRepo
}

// DerivedRepo
func (r *PgPrRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[PrRepo]) PrRepo {
	prRepo := new(PgPrRepo)
	prRepo.PgBaseRepo = *base
	prRepo.explainAnalyze = r.explainAnalyze
	return prRepo
}

func (r *PgPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (PatronRequest, error) {
	row, err := r.queries.GetPatronRequestById(ctx, r.GetConnOrTx(), id)
	return row.PatronRequest, err
}

func (r *PgPrRepo) GetPatronRequestSearchView(ctx common.ExtendedContext, id string) (PatronRequestSearchView, error) {
	row, err := r.queries.GetPatronRequestSearchView(ctx, r.GetConnOrTx(), id)
	if err != nil {
		return PatronRequestSearchView{}, err
	}
	return row.PatronRequestSearchView, nil
}

func (r *PgPrRepo) GetPatronRequestByIdForUpdate(ctx common.ExtendedContext, id string) (PatronRequest, error) {
	row, err := r.queries.GetPatronRequestByIdForUpdate(ctx, r.GetConnOrTx(), id)
	return row.PatronRequest, err
}

func (r *PgPrRepo) GetPatronRequestByIdAndSide(ctx common.ExtendedContext, id string, side PatronRequestSide) (PatronRequest, error) {
	pr, err := r.GetPatronRequestById(ctx, id)
	if err != nil {
		return PatronRequest{}, err
	}
	if pr.Side != side {
		return PatronRequest{}, pgx.ErrNoRows
	}
	return pr, nil
}

func (r *PgPrRepo) ListPatronRequests(ctx common.ExtendedContext, params ListPatronRequestsParams, pgcql pgcql.Query) ([]PatronRequest, int64, error) {
	rows, fullCount, err := r.listPatronRequestRows(ctx, params, pgcql)
	if err != nil {
		return nil, fullCount, err
	}
	list := make([]PatronRequest, 0, len(rows))
	for _, row := range rows {
		list = append(list, patronRequestFromSearchView(row.PatronRequestSearchView))
	}
	return list, fullCount, nil
}

func (r *PgPrRepo) GetPatronRequestsFacets(ctx common.ExtendedContext, facetFields []string, pgcql pgcql.Query) ([]Facet, error) {
	var facets []Facet
	for _, field := range facetFields {
		switch field {
		case "requester_symbol", "supplier_symbol", "requester_name", "supplier_name":
			rows, err := r.queries.GetPatronRequestsFacetsCql(ctx, r.GetConnOrTx(), field, pgcql)
			if err != nil {
				return nil, err
			}
			var values []FacetValue
			for _, row := range rows {
				if row.Value.Valid {
					values = append(values, FacetValue{
						Value: row.Value.String,
						Count: row.Count,
					})
				}
			}
			facets = append(facets, Facet{
				Field:  field,
				Values: values,
			})
		default:
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedFacet, field)
		}
	}
	return facets, nil
}

func (r *PgPrRepo) ListPatronRequestsSearchView(ctx common.ExtendedContext, params ListPatronRequestsParams, pgcql pgcql.Query) ([]PatronRequestSearchView, int64, error) {
	rows, fullCount, err := r.listPatronRequestRows(ctx, params, pgcql)
	if err != nil {
		return nil, fullCount, err
	}
	list := make([]PatronRequestSearchView, 0, len(rows))
	for _, row := range rows {
		list = append(list, row.PatronRequestSearchView)
	}
	return list, fullCount, nil
}

func (r *PgPrRepo) listPatronRequestRows(ctx common.ExtendedContext, params ListPatronRequestsParams, pgcql pgcql.Query) ([]ListPatronRequestsRow, int64, error) {
	db := r.GetConnOrTx()
	rows, explainResult, err := r.queries.ListPatronRequestsCql(ctx, db, params, pgcql, r.explainAnalyze)
	var fullCount int64
	if err == nil {
		for _, line := range explainResult {
			ctx.Logger().Info("explain", "line", line)
		}
		if len(rows) > 0 {
			fullCount = rows[0].FullCount
		} else {
			params.Limit = 1
			params.Offset = 0
			countRows, _, countErr := r.queries.ListPatronRequestsCql(ctx, db, params, pgcql, false)
			err = countErr
			if err == nil && len(countRows) > 0 {
				fullCount = countRows[0].FullCount
			}
		}
	}
	return rows, fullCount, err
}

func patronRequestFromSearchView(v PatronRequestSearchView) PatronRequest {
	return PatronRequest{
		ID:                v.ID,
		CreatedAt:         v.CreatedAt,
		IllRequest:        v.IllRequest,
		State:             v.State,
		Side:              v.Side,
		Patron:            v.Patron,
		RequesterSymbol:   v.RequesterSymbol,
		SupplierSymbol:    v.SupplierSymbol,
		Tenant:            v.Tenant,
		RequesterReqID:    v.RequesterReqID,
		NeedsAttention:    v.NeedsAttention,
		LastAction:        v.LastAction,
		LastActionOutcome: v.LastActionOutcome,
		LastActionResult:  v.LastActionResult,
		Language:          v.Language,
		Items:             v.Items,
		TerminalState:     v.TerminalState,
		UpdatedAt:         v.UpdatedAt,
		IllResponse:       v.IllResponse,
		InternalNote:      v.InternalNote,
		StateModel:        v.StateModel,
	}
}

func (r *PgPrRepo) UpdatePatronRequest(ctx common.ExtendedContext, params UpdatePatronRequestParams) (PatronRequest, error) {
	row, err := r.queries.UpdatePatronRequest(ctx, r.GetConnOrTx(), params)
	return row.PatronRequest, err
}

func (r *PgPrRepo) UpdatePatronRequestInternalNote(ctx common.ExtendedContext, id string, internalNote pgtype.Text) error {
	return r.queries.UpdatePatronRequestInternalNote(ctx, r.GetConnOrTx(), UpdatePatronRequestInternalNoteParams{
		ID:           id,
		InternalNote: internalNote,
	})
}
func (r *PgPrRepo) CreatePatronRequest(ctx common.ExtendedContext, params CreatePatronRequestParams) (PatronRequest, error) {
	row, err := r.queries.CreatePatronRequest(ctx, r.GetConnOrTx(), params)
	return row.PatronRequest, err
}

func (r *PgPrRepo) DeletePatronRequest(ctx common.ExtendedContext, id string) error {
	return r.queries.DeletePatronRequest(ctx, r.GetConnOrTx(), id)
}

func (r *PgPrRepo) GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx common.ExtendedContext, supplierSymbol string, requesterReId string) (PatronRequest, error) {
	row, err := r.queries.GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx, r.GetConnOrTx(), GetLendingRequestBySupplierSymbolAndRequesterReqIdParams{
		SupplierSymbol: pgtype.Text{
			String: supplierSymbol,
			Valid:  true,
		},
		RequesterReqID: pgtype.Text{
			String: requesterReId,
			Valid:  true,
		},
	})
	return row.PatronRequest, err
}

func (r *PgPrRepo) GetNextHrid(ctx common.ExtendedContext, prefix string) (string, error) {
	return r.queries.GetNextHrid(ctx, r.GetConnOrTx(), strings.ToUpper(prefix))
}

func (r *PgPrRepo) SaveItem(ctx common.ExtendedContext, params SaveItemParams) (Item, error) {
	row, err := r.queries.SaveItem(ctx, r.GetConnOrTx(), params)
	return row.Item, err
}

func (r *PgPrRepo) GetItemById(ctx common.ExtendedContext, id string) (Item, error) {
	row, err := r.queries.GetItemById(ctx, r.GetConnOrTx(), id)
	return row.Item, err
}

func (r *PgPrRepo) GetItemsByPrId(ctx common.ExtendedContext, prId string) ([]Item, error) {
	rows, err := r.queries.GetItemsByPrId(ctx, r.GetConnOrTx(), prId)
	var list []Item
	for _, row := range rows {
		list = append(list, row.Item)
	}
	return list, err
}

func (r *PgPrRepo) SaveNotification(ctx common.ExtendedContext, params SaveNotificationParams) (Notification, error) {
	row, err := r.queries.SaveNotification(ctx, r.GetConnOrTx(), params)
	return row.Notification, err
}

func (r *PgPrRepo) GetNotificationById(ctx common.ExtendedContext, id string) (Notification, error) {
	row, err := r.queries.GetNotificationById(ctx, r.GetConnOrTx(), id)
	return row.Notification, err
}

func (r *PgPrRepo) GetNotificationsByPrId(ctx common.ExtendedContext, params GetNotificationsByPrIdParams) ([]Notification, int64, error) {
	rows, err := r.queries.GetNotificationsByPrId(ctx, r.GetConnOrTx(), params)
	var list []Notification
	var fullCount int64
	if err == nil {
		if len(rows) > 0 {
			for _, row := range rows {
				list = append(list, row.Notification)
				fullCount = row.FullCount
			}
		} else {
			params.Limit = 1
			params.Offset = 0
			rows, err = r.queries.GetNotificationsByPrId(ctx, r.GetConnOrTx(), params)
			if err == nil && len(rows) > 0 {
				fullCount = rows[0].FullCount
			}
		}
	}
	return list, fullCount, err
}

func (r *PgPrRepo) MarkConditionNotificationsReceipt(ctx common.ExtendedContext, params MarkConditionNotificationsReceiptParams) error {
	return r.queries.MarkConditionNotificationsReceipt(ctx, r.GetConnOrTx(), params)
}

func (r *PgPrRepo) DeleteNotificationById(ctx common.ExtendedContext, id string) error {
	return r.queries.DeleteNotificationById(ctx, r.GetConnOrTx(), id)
}

func (r *PgPrRepo) DeleteItemById(ctx common.ExtendedContext, id string) error {
	return r.queries.DeleteItemById(ctx, r.GetConnOrTx(), id)
}

func (r *PgPrRepo) SaveTemplate(ctx common.ExtendedContext, params SaveTemplateParams) (Template, error) {
	row, err := r.queries.SaveTemplate(ctx, r.GetConnOrTx(), params)
	return row.Template, err
}

func (r *PgPrRepo) GetTemplateByIdAndOwner(ctx common.ExtendedContext, id string, owner string) (Template, error) {
	row, err := r.queries.GetTemplateByIdAndOwner(ctx, r.GetConnOrTx(), GetTemplateByIdAndOwnerParams{
		ID:    id,
		Owner: owner,
	})
	return row.Template, err
}

func (r *PgPrRepo) GetTemplatesByOwner(ctx common.ExtendedContext, params GetTemplatesByOwnerParams) ([]Template, int64, error) {
	rows, err := r.queries.GetTemplatesByOwner(ctx, r.GetConnOrTx(), params)
	var list []Template
	var fullCount int64
	for _, row := range rows {
		fullCount = row.FullCount
		list = append(list, row.Template)
	}
	return list, fullCount, err
}

func (r *PgPrRepo) GetTemplateByPurposeAudienceLabelAndOwner(ctx common.ExtendedContext, params GetTemplateByPurposeAudienceLabelAndOwnerParams) (Template, error) {
	row, err := r.queries.GetTemplateByPurposeAudienceLabelAndOwner(ctx, r.GetConnOrTx(), params)
	return row.Template, err
}

func (r *PgPrRepo) DeleteTemplateByIdAndOwner(ctx common.ExtendedContext, id string, owner string) error {
	return r.queries.DeleteTemplateByIdAndOwner(ctx, r.GetConnOrTx(), DeleteTemplateByIdAndOwnerParams{
		ID:    id,
		Owner: owner,
	})
}
