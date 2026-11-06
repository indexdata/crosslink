package sched_service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/stretchr/testify/require"
)

type transactionArchiveRepo struct {
	pages    [][]ill_db.IllTransactionArchiveCandidate
	cursors  []string
	query    pgcql.Query
	archived int
	skip     bool
	err      error
	cancel   context.CancelFunc
}

func (r *transactionArchiveRepo) ListIllTransactionArchiveCandidates(_ common.ExtendedContext, _ int32, after string, q pgcql.Query) ([]ill_db.IllTransactionArchiveCandidate, error) {
	r.cursors = append(r.cursors, after)
	r.query = q
	page := len(r.cursors) - 1
	if page >= len(r.pages) {
		return nil, nil
	}
	return r.pages[page], nil
}
func (r *transactionArchiveRepo) ArchiveIllTransaction(_ common.ExtendedContext, _ ill_db.IllTransactionArchiveCandidate, _ string, _ time.Time, _ bool) (bool, error) {
	r.archived++
	if r.cancel != nil {
		r.cancel()
	}
	if len(r.cursors) == 1 && r.skip {
		return false, r.err
	}
	return true, r.err
}
func transactionArchiveEvent() events.Event {
	event := batchActionEvent(string(schedoapi.ArchiveIllTransactions))
	event.EventData.BatchActionData.Selector = "last_supplier_status==LoanCompleted sortBy timestamp"
	event.EventData.CustomData = map[string]any{"interval": "24h", "anonymize": true}
	return event
}
func TestTransactionArchiveContinuesPastSkippedPage(t *testing.T) {
	repo := &transactionArchiveRepo{skip: true}
	page := make([]ill_db.IllTransactionArchiveCandidate, 100)
	for i := range page {
		page[i] = ill_db.IllTransactionArchiveCandidate{ID: fmt.Sprintf("%03d", i)}
	}
	repo.pages = [][]ill_db.IllTransactionArchiveCandidate{page, {{ID: "100"}}}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil).WithIllTransactionArchiveRepo(repo)
	status, result := svc.batchAction(testCtx, transactionArchiveEvent())
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 1 ILL transactions; skipped 100")
	require.Equal(t, []string{"", "099"}, repo.cursors)
	require.Contains(t, repo.query.GetWhereClause(), "requester_symbol")
	require.Contains(t, repo.query.GetQueryArguments(), testBatchActionOwner)
	require.Contains(t, repo.query.GetWhereClause(), "timestamp")
	require.NotContains(t, repo.query.GetWhereClause(), "side")
}
func TestTransactionArchiveFailureAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		repo := &transactionArchiveRepo{pages: [][]ill_db.IllTransactionArchiveCandidate{{{ID: "one"}, {ID: "two"}}}}
		if cancel {
			repo.cancel = stop
		} else {
			repo.err = errors.New("archive failed")
		}
		svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil).WithIllTransactionArchiveRepo(repo)
		status, result := svc.batchAction(common.CreateExtCtxWithArgs(ctx, nil), transactionArchiveEvent())
		require.Equal(t, events.EventStatusError, status)
		if cancel {
			require.Equal(t, 1, repo.archived)
			require.NotNil(t, result.EventError)
		} else {
			require.Len(t, result.CustomData, 2)
		}
		stop()
	}
}
func TestValidateTransactionArchiveConfig(t *testing.T) {
	require.NoError(t, ValidateArchiveConfig("archive-ill-transactions", "ISIL:OWNER", "last_supplier_status==Unfilled", map[string]any{"interval": "24h"}))
	for _, query := range []string{"state==CLOSED", "terminal_state=true", ""} {
		require.Error(t, ValidateArchiveConfig("archive-ill-transactions", "ISIL:OWNER", query, map[string]any{"interval": "24h"}))
	}
	require.Error(t, ValidateArchiveConfig("archive-ill-transactions", "", "last_supplier_status==Unfilled", map[string]any{"interval": "24h"}))
	require.Error(t, ValidateArchiveConfig("archive-ill-transactions", "ISIL:OWNER", "last_supplier_status==Unfilled", map[string]any{"interval": "0h"}))
}

func TestTransactionArchiveBoundsResults(t *testing.T) {
	for _, fail := range []bool{false, true} {
		repo := &transactionArchiveRepo{}
		for page := 0; page < 2; page++ {
			candidates := make([]ill_db.IllTransactionArchiveCandidate, 100)
			for i := range candidates {
				candidates[i].ID = fmt.Sprintf("%03d", page*100+i)
			}
			repo.pages = append(repo.pages, candidates)
		}
		if fail {
			repo.err = errors.New("archive failed")
		}
		svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil).WithIllTransactionArchiveRepo(repo)
		status, result := svc.batchAction(testCtx, transactionArchiveEvent())
		if fail {
			require.Equal(t, events.EventStatusError, status)
			require.Equal(t, 200, repo.archived)
			require.Len(t, result.CustomData, 100)
		} else {
			require.Equal(t, events.EventStatusSuccess, status)
			require.Equal(t, 100, repo.archived)
			require.Len(t, repo.cursors, 1)
		}
	}
}
