package events

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestBatchWorkerCancelsAtLastConfirmedDeadline(t *testing.T) {
	const lease = 3 * time.Second
	unavailable := errors.New("database unavailable")
	for _, tt := range []struct {
		name      string
		renew     func(start time.Time, attempt int) (time.Time, error)
		elapsed   time.Duration
		minTrials int
	}{
		{
			name: "transient error followed by renewal up to cap",
			renew: func(start time.Time, attempt int) (time.Time, error) {
				if attempt == 1 {
					return time.Time{}, unavailable
				}
				return start.Add(4 * time.Second), nil
			},
			elapsed: 4 * time.Second, minTrials: 2,
		},
		{
			name: "persistent error until lease expires",
			renew: func(time.Time, int) (time.Time, error) {
				return time.Time{}, unavailable
			},
			elapsed: lease, minTrials: 2,
		},
		{
			name: "worker no longer owns lease",
			renew: func(time.Time, int) (time.Time, error) {
				return time.Time{}, pgx.ErrNoRows
			},
			elapsed: lease / 3, minTrials: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				bus := NewPostgresEventBus(nil, "")
				bus.ctx = common.CreateExtCtxWithArgs(context.Background(), nil)
				event := Event{ID: "worker", EventName: EventNameInvokeBatchAction}
				stoppedAt := make(chan time.Time, 1)
				bus.HandleEventCreated(event.EventName, HandlerRoleConsumer, func(ctx common.ExtendedContext, _ Event) {
					<-ctx.Done()
					stoppedAt <- time.Now()
				})
				attempts := 0
				bus.runBatchWorker(t.Context(), event, start.Add(lease), lease, func(context.Context) (time.Time, error) {
					attempts++
					return tt.renew(start, attempts)
				})
				require.Equal(t, tt.elapsed, (<-stoppedAt).Sub(start))
				require.GreaterOrEqual(t, attempts, tt.minTrials)
			})
		})
	}
}
