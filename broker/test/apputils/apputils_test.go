package apputils

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/stretchr/testify/require"
)

type pendingEventRepo struct {
	events.EventRepo
}

func (pendingEventRepo) GetIllTransactionEvents(common.ExtendedContext, string) ([]events.Event, int64, error) {
	return []events.Event{{ID: "pending-event", EventType: events.EventTypeTask, EventName: events.EventNameMessageSupplier, EventStatus: events.EventStatusNew}}, 1, nil
}

func TestEventsToCompareStringReportsTimeout(t *testing.T) {
	const childFlag = "CROSSLINK_EVENT_TIMEOUT_TEST_CHILD"
	if os.Getenv(childFlag) == "1" {
		ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
		EventsToCompareStringFunc(ctx, pendingEventRepo{}, t, "waiting-transaction", 2, false, func(events.Event) string { return "" })
		t.Fatal("pending events unexpectedly passed")
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestEventsToCompareStringReportsTimeout$")
	command.Env = append(os.Environ(), childFlag+"=1")
	output, err := command.CombinedOutput()
	require.Error(t, err)
	require.NoError(t, ctx.Err())
	require.Contains(t, string(output), "timed out waiting for transaction waiting-transaction events: expected 2, got 1")
	require.Contains(t, string(output), "outstanding: [pending-event: message-supplier = NEW]")
	require.Contains(t, string(output), "observed events:")
	require.NotContains(t, string(output), "pending events unexpectedly passed")
}
