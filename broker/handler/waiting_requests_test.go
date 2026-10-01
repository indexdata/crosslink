package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/require"
)

type immediateConfirmationBus struct {
	events.EventBus
	publish func(string) error
}

func (b immediateConfirmationBus) CreateNoticeWithID(id, _ string, _ events.EventName, _ events.EventData, _ events.EventStatus, _ events.EventDomain, _ events.SignalTarget) error {
	return b.publish(id)
}

func TestCreateNoticeAndWaitConfirmsBeforePublicationReturns(t *testing.T) {
	for _, name := range []events.EventName{events.EventNameRequesterMsgReceived, events.EventNameSupplierMsgReceived} {
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()
			ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
			recorder := httptest.NewRecorder()
			handler := &Iso18626Handler{}
			var noticeID string
			bus := immediateConfirmationBus{publish: func(id string) error {
				noticeID = id
				message := iso18626.NewISO18626Message()
				result := events.EventResult{CustomData: map[string]any{common.DO_NOT_SEND: true}}
				var err error
				if name == events.EventNameRequesterMsgReceived {
					message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{Action: iso18626.TypeActionShippedReturn}
					_, err = handler.confirmSupplierResponse(ctx, "transaction", id, message, result)
				} else {
					message.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
					_, err = handler.confirmRequesterResponse(ctx, "transaction", id, message, result)
				}
				return err
			}}
			done := make(chan error, 1)
			go func() {
				done <- createNoticeAndWait(ctx, bus, "transaction", name, events.EventData{}, recorder)
			}()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("confirmation before publication returned did not release the request")
			}
			require.Equal(t, 200, recorder.Code)
			require.Contains(t, recorder.Body.String(), "MessageConfirmation")
			_, registered := waitingReqs.Load(noticeID)
			require.False(t, registered)
			_, err := handler.confirmSupplierResponse(ctx, "transaction", noticeID, nil, events.EventResult{})
			require.ErrorContains(t, err, "not found", "a second confirmation must not claim the response")
		})
	}
}

func TestCreateNoticeAndWaitRemovesWaiterOnPublicationFailure(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	wantErr := errors.New("commit failed")
	var noticeID string
	bus := immediateConfirmationBus{publish: func(id string) error {
		noticeID = id
		_, registered := waitingReqs.Load(id)
		require.True(t, registered)
		return wantErr
	}}
	err := createNoticeAndWait(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, httptest.NewRecorder())
	require.ErrorIs(t, err, wantErr)
	_, registered := waitingReqs.Load(noticeID)
	require.False(t, registered)
}
