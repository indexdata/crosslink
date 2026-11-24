package sched_db

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/repo"
)

// BatchEmailDeliveryRepo records delivery progress for a coordinated pull-slip email.
type BatchEmailDeliveryRepo interface {
	// RecordBatchEmailIntent starts the delivery once for the current root attempt.
	RecordBatchEmailIntent(ctx common.ExtendedContext, runID string, requestIDs []string, queuePrintedActions bool) error
	// RecordBatchEmailAcceptance marks delivery as accepted or confirms a previous write.
	RecordBatchEmailAcceptance(ctx common.ExtendedContext, runID string) error
}

// RecordBatchEmailIntent persists the selected requests before SMTP submission.
func (r *PgSchedRepo) RecordBatchEmailIntent(ctx common.ExtendedContext, runID string, requestIDs []string, queuePrintedActions bool) error {
	attempt := repo.BatchAttemptFrom(ctx)
	if attempt.ID == "" || attempt.EventID != runID {
		return repo.ErrBatchAttemptLost
	}
	return repo.BatchExec(ctx, &r.PgBaseRepo, func(db repo.ConnOrTx) error {
		count, err := r.eventQueries.RecordBatchEmailIntent(ctx, db, events.RecordBatchEmailIntentParams{
			RunID: runID, SelectedRequestIds: requestIDs, QueuePrintedActions: queuePrintedActions,
		})
		if err != nil {
			return fmt.Errorf("record batch email intent: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("cannot start delivery for batch run %s: expected an active email run without delivery intent", runID)
		}
		return nil
	})
}

// RecordBatchEmailAcceptance records the successful SMTP response without changing an existing acceptance timestamp.
func (r *PgSchedRepo) RecordBatchEmailAcceptance(ctx common.ExtendedContext, runID string) error {
	attempt := repo.BatchAttemptFrom(ctx)
	if attempt.ID == "" || attempt.EventID != runID {
		return repo.ErrBatchAttemptLost
	}
	return repo.BatchExec(ctx, &r.PgBaseRepo, func(db repo.ConnOrTx) error {
		count, err := r.eventQueries.RecordBatchEmailAcceptance(ctx, db, runID)
		if err != nil {
			return fmt.Errorf("record batch email acceptance: %w", err)
		}
		if count != 1 {
			return fmt.Errorf("cannot record acceptance for batch run %s: no delivery intent", runID)
		}
		return nil
	})
}
