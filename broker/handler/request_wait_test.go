package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestWaitRegistryConcurrentAccess(t *testing.T) {
	registry := requestWaitRegistry{requests: make(map[string]RequestWait)}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range 64 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			id := fmt.Sprint(i)
			var pending sync.WaitGroup
			registry.register(id, RequestWait{wg: &pending})
			assert.True(t, registry.contains(id))
			var takers sync.WaitGroup
			results := make(chan bool, 2)
			for range 2 {
				takers.Add(1)
				go func() {
					defer takers.Done()
					wait, ok := registry.take(id)
					if ok {
						assert.Same(t, &pending, wait.wg)
					}
					results <- ok
				}()
			}
			takers.Wait()
			assert.NotEqual(t, <-results, <-results, "exactly one caller must consume the waiter")
			assert.False(t, registry.contains(id))
		}()
	}
	close(start)
	workers.Wait()
	assert.Empty(t, registry.requests)
}

type immediateNoticeBus struct {
	events.EventBus
	publish func(string) error
}

func (b immediateNoticeBus) CreateNoticeWithID(eventID, _ string, _ events.EventName, _ events.EventData, _ events.EventStatus, _ events.EventDomain, _ events.SignalTarget) (string, error) {
	return eventID, b.publish(eventID)
}

func TestWaitForMessageConfirmationBeforePublishReturns(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	h := &Iso18626Handler{}
	for _, name := range []events.EventName{events.EventNameRequesterMsgReceived, events.EventNameSupplierMsgReceived} {
		t.Run(string(name), func(t *testing.T) {
			// All publications overlap, and each confirmation runs before publication returns.
			const requests = 32
			ready := make(chan struct{}, requests)
			release := make(chan struct{})
			var workers sync.WaitGroup
			for range requests {
				workers.Add(1)
				go func() {
					defer workers.Done()
					recorder := httptest.NewRecorder()
					bus := immediateNoticeBus{publish: func(id string) error {
						assert.True(t, waitingReqs.contains(id))
						ready <- struct{}{}
						<-release
						msg := iso18626.NewISO18626Message()
						result := events.EventResult{CustomData: map[string]any{common.DO_NOT_SEND: true}}
						var confirmation *iso18626.ISO18626Message
						var err error
						if name == events.EventNameRequesterMsgReceived {
							msg.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}
							confirmation, err = h.confirmSupplierResponse(ctx, "transaction", id, msg, result)
						} else {
							msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
							confirmation, err = h.confirmRequesterResponse(ctx, "transaction", id, msg, result)
						}
						if !assert.NoError(t, err) {
							return err
						}
						assert.NotNil(t, confirmation)
						assert.False(t, waitingReqs.contains(id))
						// A duplicate confirmation must not write or complete the waiter twice.
						if name == events.EventNameRequesterMsgReceived {
							_, err = h.confirmSupplierResponse(ctx, "transaction", id, msg, result)
						} else {
							_, err = h.confirmRequesterResponse(ctx, "transaction", id, msg, result)
						}
						assert.ErrorContains(t, err, "not found")
						return nil
					}}
					assert.NoError(t, waitForMessageConfirmation(ctx, bus, "transaction", name, events.EventData{}, recorder))
					assert.Equal(t, 200, recorder.Code)
					assert.Contains(t, recorder.Body.String(), "MessageConfirmation")
				}()
			}
			for range requests {
				<-ready
			}
			close(release)
			workers.Wait()
		})
	}
}

func TestWaitForMessageConfirmationPublishFailure(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	publishErr := errors.New("publication failed")
	var eventID string
	bus := immediateNoticeBus{publish: func(id string) error {
		eventID = id
		assert.True(t, waitingReqs.contains(id))
		return publishErr
	}}
	err := waitForMessageConfirmation(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, httptest.NewRecorder())
	require.ErrorIs(t, err, publishErr)
	assert.False(t, waitingReqs.contains(eventID), "failed publication must not leave a waiter")
}
