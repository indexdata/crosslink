package pr_db

import (
	"strings"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type PrRepo interface {
	repo.Transactional[PrRepo]
	GetPatronRequestById(ctx common.ExtendedContext, id string) (PatronRequest, error)
	GetPatronRequestByIdForUpdate(ctx common.ExtendedContext, id string) (PatronRequest, error)
	GetPatronRequestByIdAndSide(ctx common.ExtendedContext, id string, side PatronRequestSide) (PatronRequest, error)
	ListPatronRequests(ctx common.ExtendedContext, args ListPatronRequestsParams, cql *string) ([]PatronRequest, int64, error)
	UpdatePatronRequest(ctx common.ExtendedContext, params UpdatePatronRequestParams) (PatronRequest, error)
	CreatePatronRequest(ctx common.ExtendedContext, params CreatePatronRequestParams) (PatronRequest, error)
	DeletePatronRequest(ctx common.ExtendedContext, id string) error
	GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx common.ExtendedContext, supplierSymbol string, requesterReId string) (PatronRequest, error)
	GetNextHrid(ctx common.ExtendedContext, prefix string) (string, error)
	SaveItem(ctx common.ExtendedContext, params SaveItemParams) (Item, error)
	GetItemById(ctx common.ExtendedContext, id string) (Item, error)
	GetItemsByPrId(ctx common.ExtendedContext, prId string) ([]Item, error)
	SaveNotification(ctx common.ExtendedContext, params SaveNotificationParams) (Notification, error)
	GetNotificationById(ctx common.ExtendedContext, id string) (Notification, error)
	GetNotificationsByPrId(ctx common.ExtendedContext, prId string) ([]Notification, error)
}

type PgPrRepo struct {
	repo.PgBaseRepo[PrRepo]
	queries Queries
}

// delegate transaction handling to Base
func (r *PgPrRepo) WithTxFunc(ctx common.ExtendedContext, fn func(PrRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// DerivedRepo
func (r *PgPrRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[PrRepo]) PrRepo {
	prRepo := new(PgPrRepo)
	prRepo.PgBaseRepo = *base
	return prRepo
}

func (r *PgPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (PatronRequest, error) {
	row, err := r.queries.GetPatronRequestById(ctx, r.GetConnOrTx(), id)
	return row.PatronRequest, err
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

func (r *PgPrRepo) ListPatronRequests(ctx common.ExtendedContext, params ListPatronRequestsParams, cql *string) ([]PatronRequest, int64, error) {
	rows, err := r.queries.ListPatronRequestsCql(ctx, r.GetConnOrTx(), params, cql)
	var list []PatronRequest
	var fullCount int64
	if err == nil {
		if len(rows) > 0 {
			fullCount = rows[0].FullCount
			for _, r := range rows {
				fullCount = r.FullCount
				list = append(list, PatronRequest{
					ID:                r.ID,
					Timestamp:         r.Timestamp,
					IllRequest:        r.IllRequest,
					State:             PatronRequestState(r.State),
					Side:              PatronRequestSide(r.Side),
					Patron:            r.Patron,
					RequesterSymbol:   r.RequesterSymbol,
					SupplierSymbol:    r.SupplierSymbol,
					Tenant:            r.Tenant,
					RequesterReqID:    r.RequesterReqID,
					NeedsAttention:    r.NeedsAttention,
					LastAction:        r.LastAction,
					LastActionOutcome: r.LastActionOutcome,
					LastActionResult:  r.LastActionResult,
				})
			}
		} else {
			params.Limit = 1
			params.Offset = 0
			rows, err = r.queries.ListPatronRequestsCql(ctx, r.GetConnOrTx(), params, cql)
			if err == nil && len(rows) > 0 {
				fullCount = rows[0].FullCount
			}
		}
	}
	return list, fullCount, err
}

func (r *PgPrRepo) UpdatePatronRequest(ctx common.ExtendedContext, params UpdatePatronRequestParams) (PatronRequest, error) {
	row, err := r.queries.UpdatePatronRequest(ctx, r.GetConnOrTx(), params)
	return row.PatronRequest, err
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

func (r *PgPrRepo) GetNotificationsByPrId(ctx common.ExtendedContext, prId string) ([]Notification, error) {
	rows, err := r.queries.GetNotificationsByPrId(ctx, r.GetConnOrTx(), prId)
	var list []Notification
	for _, row := range rows {
		list = append(list, row.Notification)
	}
	return list, err
}
