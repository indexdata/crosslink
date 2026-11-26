package repo

import (
	"context"
	"errors"
	"time"

	"github.com/indexdata/crosslink/broker/common"
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
