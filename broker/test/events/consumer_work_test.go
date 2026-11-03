package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	"github.com/stretchr/testify/require"
)

func TestConsumerWorkPersistsAcrossOverlappingSignals(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	illID := apptest.GetIllTransId(t, illRepo)
	repo := eventRepo.(*events.PgEventRepo)
	// Use one lifecycle handler on the shared bus; filter by this test's object
	// so its registration does not affect other events in the integration suite.
	started := make(chan string, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, events.HandlerRoleConsumer, func(handlerCtx common.ExtendedContext, event events.Event) {
		if event.IllTransactionID != illID {
			return
		}
		defer close(finished)
		started <- event.ID
		select {
		case <-release:
		case <-handlerCtx.Done():
		}
	})
	releaseConsumer := sync.OnceFunc(func() { close(release) })
	defer releaseConsumer()
	id, err := eventBus.CreateTask(illID, events.EventNameRequesterMsgReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
	require.NoError(t, err)
	select {
	case received := <-started:
		require.Equal(t, id, received)
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not start")
	}
	var count int
	require.NoError(t, repo.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1 AND claimed`, id).Scan(&count))
	require.Equal(t, 1, count)

	_, err = eventBus.BeginTask(id, events.SignalObservers)
	require.NoError(t, err)
	_, err = eventBus.CompleteTask(id, &events.EventResult{}, events.EventStatusSuccess, events.SignalConsumers)
	require.NoError(t, err)
	// There are no completion handlers for this event. Its dispatch finishes,
	// but must not remove the still-running TASK_CREATED handoff's record.
	require.Eventually(t, func() bool {
		var lastSignal string
		if err := repo.Pool.QueryRow(ctx, `SELECT last_signal FROM event WHERE id=$1`, id).Scan(&lastSignal); err != nil {
			return false
		}
		if err := repo.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1`, id).Scan(&count); err != nil {
			return false
		}
		return lastSignal == "" && count == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, repo.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1 AND signal=$2 AND claimed`, id, events.SignalTaskCreated).Scan(&count))
	require.Equal(t, 1, count)
	releaseConsumer()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not finish")
	}
	require.Eventually(t, func() bool {
		err := repo.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1`, id).Scan(&count)
		return err == nil && count == 0
	}, 5*time.Second, 10*time.Millisecond)
}

func TestObserverSignalDoesNotPrepareConsumerWork(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	illID := apptest.GetIllTransId(t, illRepo)
	id, err := eventBus.CreateNotice(illID, events.EventNameInvokeAction, events.EventData{}, events.EventStatusSuccess, events.EventDomainIllTransaction, events.SignalObservers)
	require.NoError(t, err)
	var count int
	require.NoError(t, eventRepo.(*events.PgEventRepo).Pool.QueryRow(ctx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1`, id).Scan(&count))
	require.Zero(t, count)
}
