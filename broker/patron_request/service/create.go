package prservice

import (
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
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
