package sched_db

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/repo"
)

// BatchActionPlanPageSize bounds reads and inserts of individual plan pages.
const BatchActionPlanPageSize = 100

// BatchActionPlanEntry records the selected action or the selection error for a request.
type BatchActionPlanEntry struct {
	Position  int64  `json:"-"`
	RequestID string `json:"requestId"`
	Action    string `json:"action,omitempty"`
	Error     string `json:"error,omitempty"`
}

// IsBatchActionPlanSaved reports whether the run has a complete selection.
func (r *PgSchedRepo) IsBatchActionPlanSaved(ctx common.ExtendedContext, runID string) (bool, error) {
	return r.eventQueries.IsBatchActionPlanSaved(ctx, r.GetConnOrTx(), runID)
}

// SaveBatchActionPlan commits the complete selection before any child is queued.
func (r *PgSchedRepo) SaveBatchActionPlan(ctx common.ExtendedContext, runID string, entries []BatchActionPlanEntry) error {
	attempt := repo.BatchAttemptFrom(ctx)
	if attempt.ID == "" || attempt.EventID != runID {
		return repo.ErrBatchAttemptLost
	}
	return repo.BatchExec(ctx, &r.PgBaseRepo, func(db repo.ConnOrTx) error {
		count, err := r.eventQueries.MarkBatchActionPlanSaved(ctx, db, runID)
		if err != nil {
			return err
		}
		if count == 0 {
			saved, err := r.eventQueries.IsBatchActionPlanSaved(ctx, db, runID)
			if err != nil {
				return err
			}
			if saved {
				return nil
			}
			return fmt.Errorf("batch run %s has no saved plan", runID)
		}
		var position int64
		for page := range slices.Chunk(entries, BatchActionPlanPageSize) {
			if err := r.insertBatchPlanPage(ctx, db, runID, position, page); err != nil {
				return err
			}
			position += int64(len(page))
		}
		return nil
	})
}

// GetBatchActionPlanEntries reads the next page of a run plan in selection order.
func (r *PgSchedRepo) GetBatchActionPlanEntries(ctx common.ExtendedContext, runID string, after int64) ([]BatchActionPlanEntry, error) {
	rows, err := r.eventQueries.GetBatchActionPlanEntries(ctx, r.GetConnOrTx(), events.GetBatchActionPlanEntriesParams{
		RunID: runID, AfterPosition: after, PageSize: BatchActionPlanPageSize,
	})
	if err != nil {
		return nil, err
	}
	entries := make([]BatchActionPlanEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, BatchActionPlanEntry{
			Position: row.Position, RequestID: row.RequestID, Action: row.ActionName.String, Error: row.SelectionError.String,
		})
	}
	return entries, nil
}

// OverduePlanProgress is the persisted cutoff and last scanned request of an overdue run.
type OverduePlanProgress struct {
	Cutoff   time.Time
	Cursor   string
	Complete bool
}

// StartOverdueBatchPlan fixes the cutoff once and returns the saved scan position.
func (r *PgSchedRepo) StartOverdueBatchPlan(ctx common.ExtendedContext, runID string) (OverduePlanProgress, error) {
	attempt := repo.BatchAttemptFrom(ctx)
	if attempt.ID == "" || attempt.EventID != runID {
		return OverduePlanProgress{}, repo.ErrBatchAttemptLost
	}
	return repo.BatchWrite(ctx, &r.PgBaseRepo, func(db repo.ConnOrTx) (OverduePlanProgress, error) {
		row, err := r.eventQueries.StartOverdueBatchPlan(ctx, db, runID)
		return OverduePlanProgress{Cutoff: row.OverdueCutoff.Time, Cursor: row.OverdueCursor, Complete: row.PlanSaved}, err
	})
}

// SaveOverdueBatchPlanPage commits the selected actions and cursor in one short transaction.
func (r *PgSchedRepo) SaveOverdueBatchPlanPage(ctx common.ExtendedContext, runID, previous, next string, entries []BatchActionPlanEntry, complete bool) error {
	attempt := repo.BatchAttemptFrom(ctx)
	if attempt.ID == "" || attempt.EventID != runID {
		return repo.ErrBatchAttemptLost
	}
	if len(entries) > BatchActionPlanPageSize || (!complete && next == previous) {
		return fmt.Errorf("invalid overdue plan page for run %s", runID)
	}
	return repo.BatchExec(ctx, &r.PgBaseRepo, func(db repo.ConnOrTx) error {
		count := int64(len(entries))
		size, err := r.eventQueries.AdvanceOverdueBatchPlan(ctx, db, events.AdvanceOverdueBatchPlanParams{
			RunID: runID, PreviousCursor: previous, NextCursor: next, EntryCount: count, Complete: complete,
		})
		if err != nil {
			return fmt.Errorf("advance overdue plan: %w", err)
		}
		return r.insertBatchPlanPage(ctx, db, runID, size-count, entries)
	})
}

func (r *PgSchedRepo) insertBatchPlanPage(ctx common.ExtendedContext, db repo.ConnOrTx, runID string, position int64, entries []BatchActionPlanEntry) error {
	if len(entries) == 0 {
		return nil
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode batch plan entries: %w", err)
	}
	if err = r.eventQueries.InsertBatchActionPlanEntries(ctx, db, events.InsertBatchActionPlanEntriesParams{
		RunID: runID, PageStart: position, Entries: raw,
	}); err != nil {
		return fmt.Errorf("persist batch plan entries: %w", err)
	}
	return nil
}
