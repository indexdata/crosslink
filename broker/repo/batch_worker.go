package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5"
)

type batchWorkerKey struct{}

type batchWorker struct {
	id       string
	deadline func() time.Time
}

// WithBatchWorker associates a handler and its inline children with a leased event.
func WithBatchWorker(ctx common.ExtendedContext, eventID string, deadline func() time.Time) common.ExtendedContext {
	args := ctx.LoggerArgs()
	return common.CreateExtCtxWithArgs(context.WithValue(ctx, batchWorkerKey{}, batchWorker{eventID, deadline}), &args)
}

// BatchWorker returns the leased event ID, if the context belongs to a batch worker.
func BatchWorker(ctx context.Context) string {
	worker, _ := ctx.Value(batchWorkerKey{}).(batchWorker)
	return worker.id
}

// BatchIOContext bounds external I/O by the worker's last confirmed lease.
func BatchIOContext(ctx common.ExtendedContext) (common.ExtendedContext, context.CancelFunc) {
	worker, _ := ctx.Value(batchWorkerKey{}).(batchWorker)
	if worker.deadline == nil {
		return ctx, func() {}
	}
	bounded, cancel := context.WithDeadline(ctx, worker.deadline())
	args := ctx.LoggerArgs()
	return common.CreateExtCtxWithArgs(bounded, &args), cancel
}

// ErrBatchLeaseLost indicates that a worker's lease can no longer authorize work.
var ErrBatchLeaseLost = errors.New("batch worker lease expired")

// LockBatchWorker fences database effects through transaction commit.
func LockBatchWorker(ctx context.Context, tx pgx.Tx) error {
	id := BatchWorker(ctx)
	if id == "" {
		return nil
	}
	var current bool
	if err := tx.QueryRow(ctx, "SELECT lock_batch_worker($1)", id).Scan(&current); err != nil {
		return fmt.Errorf("check batch worker lease: %w", err)
	}
	if !current {
		return ErrBatchLeaseLost
	}
	return nil
}

// BatchWrite adds a lease check to a mutation performed by a batch worker.
func BatchWrite[T, R any](ctx common.ExtendedContext, r *PgBaseRepo[R], write func(ConnOrTx) (T, error)) (T, error) {
	if BatchWorker(ctx) == "" {
		return write(r.GetConnOrTx())
	}
	var result T
	fencedWrite := func(tx pgx.Tx) error {
		if err := LockBatchWorker(ctx, tx); err != nil {
			return err
		}
		var err error
		result, err = write(tx)
		return err
	}
	var err error
	if r.Tx != nil {
		err = fencedWrite(r.Tx)
	} else {
		err = pgx.BeginFunc(ctx, r.Pool, fencedWrite)
	}
	return result, err
}

// BatchExec fences a mutation that returns only an error.
func BatchExec[R any](ctx common.ExtendedContext, r *PgBaseRepo[R], write func(ConnOrTx) error) error {
	_, err := BatchWrite(ctx, r, func(db ConnOrTx) (struct{}, error) {
		return struct{}{}, write(db)
	})
	return err
}
