package events

import (
	"context"
	"time"

	"github.com/indexdata/crosslink/broker/common"
)

func (r *PgEventRepo) recoverMessageConfirmations(ctx common.ExtendedContext) {
	const interval = 15 * time.Second
	const limit = 100

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, interval)
		err := r.queries.ExpireMessageConfirmationTasks(queryCtx, r.Pool, limit)
		cancel()
		if err != nil && ctx.Err() == nil {
			ctx.Logger().Error("expire ISO 18626 confirmation tasks", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
