package db

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
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
