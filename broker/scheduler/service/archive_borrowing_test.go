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
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type recordingArchiveRepo struct {
	owner     string
	anonymize bool
	called    int
	err       error
	pages     [][]pr_db.PatronRequest
	query     pgcql.Query
	cursors   []*pr_db.PatronRequestArchiveCursor
	stored    map[string]bool
	errByID   map[string]error
	onArchive func()
	side      pr_db.PatronRequestSide
}

func (r *recordingArchiveRepo) ListPatronRequestArchiveCandidates(_ common.ExtendedContext, _ int32, cursor *pr_db.PatronRequestArchiveCursor, q pgcql.Query) ([]pr_db.PatronRequest, error) {
	r.query = q
	r.cursors = append(r.cursors, cursor)
	page := len(r.cursors) - 1
	if page >= len(r.pages) {
		return nil, nil
	}
	return r.pages[page], nil
}

func (r *recordingArchiveRepo) ArchiveBorrowingRequest(_ common.ExtendedContext, pr pr_db.PatronRequest, owner string, _ time.Time, anonymize bool) (bool, error) {
	r.side = "borrowing"
	return r.archive(pr, owner, anonymize)
}

func (r *recordingArchiveRepo) ArchiveLendingRequest(_ common.ExtendedContext, pr pr_db.PatronRequest, owner string, _ time.Time, anonymize bool) (bool, error) {
	r.side = "lending"
	return r.archive(pr, owner, anonymize)
}

func (r *recordingArchiveRepo) archive(pr pr_db.PatronRequest, owner string, anonymize bool) (bool, error) {
	r.called++
	if r.onArchive != nil {
		r.onArchive()
	}
	r.owner, r.anonymize = owner, anonymize
	if err := r.errByID[pr.ID]; err != nil {
		return false, err
	}
	if r.stored != nil {
		return r.stored[pr.ID], r.err
	}
	return r.err == nil, r.err
}

func TestValidatePatronRequestArchiveConfig(t *testing.T) {
	for _, name := range []schedoapi.BatchActionName{schedoapi.ArchiveBorrowingRequests, schedoapi.ArchiveLendingRequests} {
		for _, tc := range []struct {
			name, owner, selector string
			params                map[string]any
			valid                 bool
		}{
			{"valid", "ISIL:TEST", "state==CLOSED", map[string]any{"interval": "720h", "anonymize": true}, true},
			{"missing owner", "", "state==CLOSED", map[string]any{"interval": "720h"}, false},
			{"invalid selector", "ISIL:TEST", "unknownField==CLOSED", map[string]any{"interval": "720h"}, false},
			{"missing interval", "ISIL:TEST", "state==CLOSED", nil, false},
			{"zero interval", "ISIL:TEST", "state==CLOSED", map[string]any{"interval": "0h"}, false},
			{"invalid anonymize", "ISIL:TEST", "state==CLOSED", map[string]any{"interval": "720h", "anonymize": "true"}, false},
		} {
			t.Run(string(name)+"/"+tc.name, func(t *testing.T) {
				err := ValidatePatronRequestArchiveConfig(string(name), tc.owner, tc.selector, tc.params)
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestArchiveBorrowingRequestsSelectionAndHistory(t *testing.T) {
	archive := &recordingArchiveRepo{pages: [][]pr_db.PatronRequest{{{ID: "pr-1", State: "CLOSED", UpdatedAt: pgtype.Timestamp{Time: time.Now().Add(-48 * time.Hour), Valid: true}}}}}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
	event := batchActionEvent(string(schedoapi.ArchiveBorrowingRequests))
	event.EventData.BatchActionData.Selector = "state==CLOSED"
	event.EventData.CustomData = map[string]any{"interval": "24h", "anonymize": true}
	status, result := svc.batchAction(testCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 1 borrowing requests")
	require.Equal(t, 1, archive.called)
	require.Equal(t, testBatchActionOwner, archive.owner)
	require.True(t, archive.anonymize)
	require.Contains(t, archive.query.GetWhereClause(), "side")
	require.Contains(t, archive.query.GetWhereClause(), "requester_symbol")
	require.Contains(t, archive.query.GetWhereClause(), "updated_at")
}

func TestArchiveLendingRequestsSelectionAndHistory(t *testing.T) {
	archive := &recordingArchiveRepo{pages: [][]pr_db.PatronRequest{{{ID: "lending-pr", State: "COMPLETED"}}}}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
	event := batchActionEvent(string(schedoapi.ArchiveLendingRequests))
	event.EventData.BatchActionData.Selector = "terminal_state=true"
	event.EventData.CustomData = map[string]any{"interval": "720h", "anonymize": true}
	status, result := svc.batchAction(testCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 1 lending requests")
	require.Equal(t, pr_db.PatronRequestSide("lending"), archive.side)
	require.Equal(t, testBatchActionOwner, archive.owner)
	require.True(t, archive.anonymize)
	require.Contains(t, archive.query.GetWhereClause(), "supplier_symbol")
	require.NotContains(t, archive.query.GetWhereClause(), "requester_symbol")
	require.Contains(t, archive.query.GetQueryArguments(), "lending")
	require.Contains(t, archive.query.GetQueryArguments(), testBatchActionOwner)
}

func TestArchiveBorrowingRequestsReportsFailure(t *testing.T) {
	archive := &recordingArchiveRepo{err: errors.New("database failed"), pages: [][]pr_db.PatronRequest{{{ID: "pr-1", State: "CLOSED"}}}}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
	event := batchActionEvent(string(schedoapi.ArchiveBorrowingRequests))
	event.EventData.CustomData = map[string]any{"interval": "24h"}
	status, result := svc.batchAction(testCtx, event)
	require.Equal(t, events.EventStatusError, status)
	require.Equal(t, "database failed", result.CustomData["pr-1"])
}

func TestArchiveBorrowingRequestsAdvancesPastSkippedAndFailedPages(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("failures=%t", fail), func(t *testing.T) {
			firstPage := make([]pr_db.PatronRequest, archiveBatchSize)
			created := pgtype.Timestamp{Time: time.Now().Add(-48 * time.Hour), Valid: true}
			archive := &recordingArchiveRepo{stored: map[string]bool{"eligible": true}, errByID: map[string]error{}}
			for i := range firstPage {
				id := fmt.Sprintf("blocked-%03d", i)
				firstPage[i] = pr_db.PatronRequest{ID: id, CreatedAt: created}
				if fail {
					archive.errByID[id] = errors.New("archive failed")
				}
			}
			archive.pages = [][]pr_db.PatronRequest{firstPage, {{ID: "eligible", CreatedAt: created}}}
			svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
			event := batchActionEvent(string(schedoapi.ArchiveBorrowingRequests))
			event.EventData.CustomData = map[string]any{"interval": "24h"}
			status, result := svc.batchAction(testCtx, event)
			if fail {
				require.Equal(t, events.EventStatusError, status)
				require.Len(t, result.CustomData, int(archiveBatchSize))
			} else {
				require.Equal(t, events.EventStatusSuccess, status)
			}
			require.Contains(t, result.Note, "archived 1 borrowing requests")
			require.Equal(t, 101, archive.called)
			require.Len(t, archive.cursors, 2)
			require.Nil(t, archive.cursors[0])
			require.Equal(t, "blocked-099", archive.cursors[1].ID)
			require.Equal(t, created.Time, archive.cursors[1].CreatedAt)
		})
	}
}

func TestArchiveBorrowingRequestsStopsAtArchiveLimit(t *testing.T) {
	page := make([]pr_db.PatronRequest, archiveBatchSize)
	for i := range page {
		page[i].ID = fmt.Sprintf("pr-%03d", i)
	}
	archive := &recordingArchiveRepo{pages: [][]pr_db.PatronRequest{page}}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
	event := batchActionEvent(string(schedoapi.ArchiveBorrowingRequests))
	event.EventData.CustomData = map[string]any{"interval": "24h"}
	status, result := svc.batchAction(testCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 100 borrowing requests")
	require.Equal(t, 100, archive.called)
	require.Len(t, archive.cursors, 1)
}

func TestArchiveBorrowingRequestsBoundsFailureHistory(t *testing.T) {
	archive := &recordingArchiveRepo{err: errors.New("archive failed")}
	for page := range 3 {
		candidates := make([]pr_db.PatronRequest, 100)
		for i := range candidates {
			candidates[i].ID = fmt.Sprintf("pr-%d-%03d", page, i)
		}
		archive.pages = append(archive.pages, candidates)
	}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
	event := batchActionEvent(string(schedoapi.ArchiveBorrowingRequests))
	event.EventData.CustomData = map[string]any{"interval": "24h"}
	status, result := svc.batchAction(testCtx, event)
	require.Equal(t, events.EventStatusError, status)
	require.Equal(t, 300, archive.called)
	require.Len(t, result.CustomData, 100)
	require.Contains(t, result.Note, "failed 300 requests")
}

func TestArchiveBorrowingRequestsCancellationRetainsProgress(t *testing.T) {
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := common.CreateExtCtxWithArgs(baseCtx, nil)
	archive := &recordingArchiveRepo{pages: [][]pr_db.PatronRequest{{{ID: "first"}, {ID: "second"}}}, onArchive: cancel}
	svc := NewBatchActionService(nil, nil, &mockBatchActionCleanupRepo{}, nil, archive)
	event := batchActionEvent(string(schedoapi.ArchiveBorrowingRequests))
	event.EventData.CustomData = map[string]any{"interval": "24h"}
	status, result := svc.batchAction(ctx, event)
	require.Equal(t, events.EventStatusError, status)
	require.Equal(t, 1, archive.called)
	require.Contains(t, result.Note, "archived 1 borrowing requests")
	require.NotNil(t, result.EventError)
	require.Equal(t, context.Canceled.Error(), result.EventError.Cause)
}
