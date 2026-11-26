package handler

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/test/mocks"
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
			pending := make(chan messageConfirmation, 1)
			registry.register(id, RequestWait{response: pending})
			assert.True(t, registry.contains(id))
			var takers sync.WaitGroup
			results := make(chan bool, 2)
			for range 2 {
				takers.Add(1)
				go func() {
					defer takers.Done()
					wait, ok := registry.take(id)
					if ok {
						assert.Equal(t, pending, wait.response)
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

func TestWaitForMessageConfirmationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var eventID string
	bus := immediateNoticeBus{publish: func(id string) error {
		eventID = id
		cancel()
		return nil
	}}
	recorder := httptest.NewRecorder()
	err := waitForMessageConfirmation(common.CreateExtCtxWithArgs(ctx, nil), bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, recorder)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, waitingReqs.contains(eventID))
	assert.Empty(t, recorder.Body.String())
}

func TestWaitForMessageConfirmationAllowsSlowResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := common.CreateExtCtxWithArgs(t.Context(), nil)
		h := &Iso18626Handler{}
		bus := immediateNoticeBus{publish: func(id string) error {
			time.AfterFunc(45*time.Second, func() {
				msg := iso18626.NewISO18626Message()
				msg.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}
				result := events.EventResult{CustomData: map[string]any{common.DO_NOT_SEND: true}}
				_, err := h.confirmSupplierResponse(ctx, "transaction", id, msg, result)
				assert.NoError(t, err)
			})
			return nil
		}}
		recorder := httptest.NewRecorder()
		err := waitForMessageConfirmation(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, recorder)
		require.NoError(t, err)
		assert.Contains(t, recorder.Body.String(), "MessageConfirmation")
	})
}

func TestISOConfirmationWaitIncludesRequestBodyRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		msg := iso18626.NewISO18626Message()
		msg.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "test-request",
				SupplyingAgencyId:         iso18626.TypeAgencyId{AgencyIdType: iso18626.TypeSchemeValuePair{Text: "ISIL"}, AgencyIdValue: "SUP"},
			},
			Action: iso18626.TypeActionNotification,
		}
		body, err := xml.Marshal(msg)
		require.NoError(t, err)
		reader, writer := io.Pipe()
		defer reader.Close()
		go func() {
			defer writer.Close()
			time.Sleep(12 * time.Second)
			_, _ = writer.Write(body)
		}()
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/iso18626", reader)
		request.Header.Set("Content-Type", "application/xml")
		start := time.Now()
		var publishedAfter time.Duration
		bus := immediateNoticeBus{publish: func(string) error {
			publishedAfter = time.Since(start)
			return nil
		}}

		response := httptest.NewRecorder()
		Iso18626PostHandler(&mocks.MockIllRepositorySuccess{}, bus, nil, 1<<20)(response, request)

		require.Equal(t, 12*time.Second, publishedAfter)
		require.Equal(t, 55*time.Second, time.Since(start))
		require.Equal(t, http.StatusInternalServerError, response.Code)
	})
}

type waitingNoticeRepo struct {
	events.EventRepo
}

func (waitingNoticeRepo) WithTxFunc(ctx common.ExtendedContext, _ func(events.EventRepo) error) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestConfirmationBudgetBoundsNoticePublication(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		parent, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		ctx := common.CreateExtCtxWithArgs(parent, nil)
		bus := events.NewPostgresEventBus(waitingNoticeRepo{}, "")
		err := waitForMessageConfirmation(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, httptest.NewRecorder())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 3*time.Second, time.Since(start))
	})
}
