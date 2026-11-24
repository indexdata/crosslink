package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5"
)

type batchAttemptKey struct{}

type batchAttemptContext struct {
	common.ExtendedContext
	attempt BatchAttempt
}

func (ctx batchAttemptContext) Value(key any) any {
	if _, ok := key.(batchAttemptKey); ok {
		return ctx.attempt
	}
	return ctx.ExtendedContext.Value(key)
}

func (ctx batchAttemptContext) WithArgs(args *common.LoggerArgs) common.ExtendedContext {
	return batchAttemptContext{ctx.ExtendedContext.WithArgs(args), ctx.attempt}
}

// BatchAttempt identifies one worker claim for a coordinated event.
type BatchAttempt struct {
	EventID string
	ID      string
}

// ErrBatchAttemptLost indicates that the worker no longer owns the event.
var ErrBatchAttemptLost = errors.New("batch action worker attempt is not current")

// WithBatchAttempt attaches the persisted claim to the worker's context.
func WithBatchAttempt(ctx common.ExtendedContext, attempt BatchAttempt) common.ExtendedContext {
	return batchAttemptContext{ctx, attempt}
}

// BatchAttemptFrom returns the coordinated worker claim, if present.
func BatchAttemptFrom(ctx context.Context) BatchAttempt {
	attempt, _ := ctx.Value(batchAttemptKey{}).(BatchAttempt)
	return attempt
}

// LockBatchAttempt fences a coordinated write for the lifetime of the transaction.
func LockBatchAttempt(ctx context.Context, tx pgx.Tx) error {
	attempt := BatchAttemptFrom(ctx)
	if attempt.ID == "" {
		return nil
	}
	var current bool
	if err := tx.QueryRow(ctx, "SELECT lock_batch_action_attempt($1, $2)", attempt.EventID, attempt.ID).Scan(&current); err != nil {
		return fmt.Errorf("lock batch action attempt: %w", err)
	}
	if !current {
		return ErrBatchAttemptLost
	}
	return nil
}

// BatchWrite runs an otherwise standalone write in a short fenced transaction
// when it is performed by a coordinated worker.
func BatchWrite[T, R any](ctx common.ExtendedContext, r *PgBaseRepo[R], write func(ConnOrTx) (T, error)) (T, error) {
	if BatchAttemptFrom(ctx).ID == "" {
		return write(r.GetConnOrTx())
	}
	var result T
	if r.Tx != nil {
		if err := LockBatchAttempt(ctx, r.Tx); err != nil {
			return result, err
		}
		return write(r.Tx)
	}
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		if err := LockBatchAttempt(ctx, tx); err != nil {
			return err
		}
		var err error
		result, err = write(tx)
		return err
	})
	return result, err
}

// BatchExec is BatchWrite for mutations that return only an error.
func BatchExec[R any](ctx common.ExtendedContext, r *PgBaseRepo[R], write func(ConnOrTx) error) error {
	_, err := BatchWrite(ctx, r, func(db ConnOrTx) (struct{}, error) {
		return struct{}{}, write(db)
	})
	return err
}
