package db

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func predecessorRequest(t *testing.T) pr_db.PatronRequest {
	t.Helper()
	id := uuid.NewString()
	t.Cleanup(func() { assert.NoError(t, prRepo.DeletePatronRequest(appCtx, id)) })
	return pr_db.PatronRequest{
		ID: id, Side: prservice.SideBorrowing,
		State: prservice.BorrowerStateCancelled, StateModel: "default",
		RequesterSymbol: pgtype.Text{String: "ISIL:LINK-TEST", Valid: true},
		CreatedAt:       pgtype.Timestamp{Time: time.Now(), Valid: true},
		Language:        "english", Items: []pr_db.PrItem{},
	}
}

func TestCreateBorrowingRequestConcurrentSuccessors(t *testing.T) {
	previous := predecessorRequest(t)
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams(previous))
	require.NoError(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	requests := [2]pr_db.PatronRequest{predecessorRequest(t), predecessorRequest(t)}
	results := [2]error{}
	for i := range requests {
		requests[i].PrevReqID = pgtype.Text{String: previous.ID, Valid: true}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, results[i] = prservice.CreateBorrowingRequest(appCtx, prRepo, requests[i])
		}()
	}
	close(start)
	wg.Wait()
	winner := 0
	if results[0] != nil {
		winner = 1
	}
	require.NoError(t, results[winner])
	require.ErrorIs(t, results[1-winner], prservice.ErrInvalidPredecessor)
	stored, err := prRepo.GetPatronRequestById(appCtx, previous.ID)
	require.NoError(t, err)
	assert.Equal(t, requests[winner].ID, stored.NextReqID.String)
	next, err := prRepo.GetPatronRequestById(appCtx, requests[winner].ID)
	require.NoError(t, err)
	assert.Equal(t, previous.ID, next.PrevReqID.String)
	_, err = prRepo.GetPatronRequestById(appCtx, requests[1-winner].ID)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

var errLinkUpdate = errors.New("injected predecessor update failure")

type failingLinkRepo struct{ pr_db.PrRepo }

func (r failingLinkRepo) WithTxFunc(ctx common.ExtendedContext, fn func(pr_db.PrRepo) error) error {
	return r.PrRepo.WithTxFunc(ctx, func(tx pr_db.PrRepo) error { return fn(failingLinkRepo{tx}) })
}

func (r failingLinkRepo) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	return pr_db.PatronRequest{}, errLinkUpdate
}

func TestCreateBorrowingRequestRollsBackLinkFailure(t *testing.T) {
	previous := predecessorRequest(t)
	_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams(previous))
	require.NoError(t, err)
	next := predecessorRequest(t)
	next.PrevReqID = pgtype.Text{String: previous.ID, Valid: true}
	_, err = prservice.CreateBorrowingRequest(appCtx, failingLinkRepo{prRepo}, next)
	require.ErrorIs(t, err, errLinkUpdate)
	_, err = prRepo.GetPatronRequestById(appCtx, next.ID)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
	stored, err := prRepo.GetPatronRequestById(appCtx, previous.ID)
	require.NoError(t, err)
	assert.False(t, stored.NextReqID.Valid)
}

func lendingPredecessorRequest(t *testing.T) pr_db.PatronRequest {
	request := predecessorRequest(t)
	request.Side = prservice.SideLending
	request.State = prservice.LenderStateUnfilled
	request.SupplierSymbol = pgtype.Text{String: "ISIL:LINK-SUP", Valid: true}
	request.RequesterReqID = pgtype.Text{String: uuid.NewString(), Valid: true}
	return request
}

func lendingSuccessor(t *testing.T, previous pr_db.PatronRequest, requestType iso18626.TypeRequestType) pr_db.PatronRequest {
	request := lendingPredecessorRequest(t)
	request.IllRequest.ServiceInfo = &iso18626.ServiceInfo{RequestType: &requestType, RequestingAgencyPreviousRequestId: previous.RequesterReqID.String}
	if requestType == "" {
		// An empty test case represents omission, rather than an explicit empty type.
		request.IllRequest.ServiceInfo.RequestType = nil
	}
	return request
}

func TestCreateLendingRequestConcurrentSuccessors(t *testing.T) {
	for _, requestType := range []iso18626.TypeRequestType{iso18626.TypeRequestTypeRetry, iso18626.TypeRequestTypeNew, ""} {
		t.Run(string(requestType), func(t *testing.T) {
			for _, duplicate := range []bool{false, true} {
				t.Run(fmt.Sprintf("duplicate=%v", duplicate), func(t *testing.T) {
					previous := lendingPredecessorRequest(t)
					_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams(previous))
					require.NoError(t, err)
					requests := [2]pr_db.PatronRequest{lendingSuccessor(t, previous, requestType), lendingSuccessor(t, previous, requestType)}
					if duplicate {
						requests[1].RequesterReqID = requests[0].RequesterReqID
					}
					start := make(chan struct{})
					var wg sync.WaitGroup
					results := [2]error{}
					for i := range requests {
						wg.Add(1)
						go func() {
							defer wg.Done()
							<-start
							_, results[i] = prservice.CreateLendingRequest(appCtx, prRepo, requests[i])
						}()
					}
					close(start)
					wg.Wait()
					winner := 0
					if results[0] != nil {
						winner = 1
					}
					require.NoError(t, results[winner])
					require.ErrorIs(t, results[1-winner], prservice.ErrInvalidPredecessor)
					stored, err := prRepo.GetPatronRequestById(appCtx, previous.ID)
					require.NoError(t, err)
					assert.Equal(t, requests[winner].ID, stored.NextReqID.String)
					next, err := prRepo.GetPatronRequestById(appCtx, requests[winner].ID)
					require.NoError(t, err)
					assert.Equal(t, previous.ID, next.PrevReqID.String)
					_, err = prRepo.GetPatronRequestById(appCtx, requests[1-winner].ID)
					assert.ErrorIs(t, err, pgx.ErrNoRows)
				})
			}
		})
	}
}

func TestCreateLendingRequestRollsBackLinkFailure(t *testing.T) {
	for _, requestType := range []iso18626.TypeRequestType{iso18626.TypeRequestTypeRetry, iso18626.TypeRequestTypeNew, ""} {
		t.Run(string(requestType), func(t *testing.T) {
			previous := lendingPredecessorRequest(t)
			_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams(previous))
			require.NoError(t, err)
			next := lendingSuccessor(t, previous, requestType)
			_, err = prservice.CreateLendingRequest(appCtx, failingLinkRepo{prRepo}, next)
			require.ErrorIs(t, err, errLinkUpdate)
			_, err = prRepo.GetPatronRequestById(appCtx, next.ID)
			assert.ErrorIs(t, err, pgx.ErrNoRows)
			stored, err := prRepo.GetPatronRequestById(appCtx, previous.ID)
			require.NoError(t, err)
			assert.False(t, stored.NextReqID.Valid)
		})
	}
}

func TestCreateLendingRequestPredecessorScope(t *testing.T) {
	for _, requestType := range []iso18626.TypeRequestType{iso18626.TypeRequestTypeRetry, iso18626.TypeRequestTypeNew, ""} {
		t.Run(string(requestType), func(t *testing.T) {
			for _, field := range []string{"requester", "supplier", "tenant", "side", "missing"} {
				t.Run(field, func(t *testing.T) {
					previous := lendingPredecessorRequest(t)
					next := lendingSuccessor(t, previous, requestType)
					switch field {
					case "requester":
						previous.RequesterSymbol.String = "ISIL:OTHER"
					case "supplier":
						previous.SupplierSymbol.String = "ISIL:OTHER"
					case "tenant":
						previous.Tenant = pgtype.Text{String: "other", Valid: true}
					case "side":
						previous.Side = prservice.SideBorrowing
					case "missing":
						next.IllRequest.ServiceInfo.RequestingAgencyPreviousRequestId = uuid.NewString()
					}
					_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams(previous))
					require.NoError(t, err)
					created, err := prservice.CreateLendingRequest(appCtx, prRepo, next)
					require.NoError(t, err)
					assert.False(t, created.PrevReqID.Valid)
					stored, err := prRepo.GetPatronRequestById(appCtx, previous.ID)
					require.NoError(t, err)
					assert.False(t, stored.NextReqID.Valid)
				})
			}
		})
	}
}
