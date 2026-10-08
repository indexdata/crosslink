package prservice

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidPredecessor indicates that a requested predecessor link is invalid.
var ErrInvalidPredecessor = errors.New("invalid predecessor")

// CreateBorrowingRequest creates a request and, when specified, links its predecessor
// atomically. The requester symbol must already have been resolved and authorized.
func CreateBorrowingRequest(ctx common.ExtendedContext, repo pr_db.PrRepo, request pr_db.PatronRequest) (pr_db.PatronRequest, error) {
	if !request.PrevReqID.Valid {
		return repo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams(request))
	}
	if request.PrevReqID.String == "" || request.PrevReqID.String == request.ID {
		return pr_db.PatronRequest{}, fmt.Errorf("%w: prevReqId must identify another request", ErrInvalidPredecessor)
	}
	var created pr_db.PatronRequest
	err := repo.WithTxFunc(ctx, func(txRepo pr_db.PrRepo) error {
		previous, err := txRepo.GetPatronRequestByIdForUpdate(ctx, request.PrevReqID.String)
		if err != nil {
			return fmt.Errorf("load predecessor: %w", err)
		}
		// Ownership follows the authorized requester symbol, as in the API's resolver.
		if request.Side != SideBorrowing || previous.Side != SideBorrowing || !request.RequesterSymbol.Valid || previous.RequesterSymbol != request.RequesterSymbol {
			return pgx.ErrNoRows
		}
		if previous.NextReqID.Valid {
			return fmt.Errorf("%w: request already has a successor", ErrInvalidPredecessor)
		}
		created, err = txRepo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams(request))
		if err != nil {
			return fmt.Errorf("create successor: %w", err)
		}
		previous.NextReqID = getDbText(created.ID)
		if _, err := txRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(previous)); err != nil {
			return fmt.Errorf("link predecessor: %w", err)
		}
		return nil
	})
	if err != nil {
		return pr_db.PatronRequest{}, err
	}
	return created, nil
}

// CreateLendingRequest creates a supply request and atomically links a retry or rerequest to
// its local predecessor, if held by the same supplier for the same requester.
func CreateLendingRequest(ctx common.ExtendedContext, repo pr_db.PrRepo, request pr_db.PatronRequest) (pr_db.PatronRequest, error) {
	info := request.IllRequest.ServiceInfo
	if info == nil || info.RequestType == nil || (*info.RequestType != iso18626.TypeRequestTypeRetry && *info.RequestType != iso18626.TypeRequestTypeNew) || info.RequestingAgencyPreviousRequestId == "" {
		return repo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams(request))
	}
	var created pr_db.PatronRequest
	err := repo.WithTxFunc(ctx, func(txRepo pr_db.PrRepo) error {
		previous, err := txRepo.GetLendingPredecessorForUpdate(ctx, pr_db.GetLendingPredecessorForUpdateParams{
			SupplierSymbol: request.SupplierSymbol, RequesterSymbol: request.RequesterSymbol,
			RequesterReqID: getDbText(info.RequestingAgencyPreviousRequestId), Tenant: request.Tenant,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("load lending predecessor: %w", err)
		}
		found := err == nil
		if found {
			// The predecessor lock serializes competing successors and prevents replacing
			// an existing successor when a message is delivered more than once.
			if previous.NextReqID.Valid {
				return fmt.Errorf("%w: request already has a successor", ErrInvalidPredecessor)
			}
			request.PrevReqID = getDbText(previous.ID)
		}
		created, err = txRepo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams(request))
		if err != nil {
			return fmt.Errorf("create lending request: %w", err)
		}
		if found {
			previous.NextReqID = getDbText(created.ID)
			if _, err := txRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(previous)); err != nil {
				return fmt.Errorf("link lending predecessor: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return pr_db.PatronRequest{}, err
	}
	return created, nil
}
