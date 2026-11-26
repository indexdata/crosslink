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
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
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

func (b immediateNoticeBus) CreateNoticeWithID(eventID, _ string, _ events.EventName, _ events.EventData, _ events.EventStatus, _ events.EventDomain, _ *string, _ events.SignalTarget) (string, error) {
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
					assert.NoError(t, waitForMessageConfirmation(ctx, bus, "transaction", name, events.EventData{}, recorder, nil))
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
	err := waitForMessageConfirmation(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, httptest.NewRecorder(), nil)
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
	err := waitForMessageConfirmation(common.CreateExtCtxWithArgs(ctx, nil), bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, recorder, nil)
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
		err := waitForMessageConfirmation(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, recorder, nil)
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
		err := waitForMessageConfirmation(ctx, bus, "transaction", events.EventNameRequesterMsgReceived, events.EventData{}, httptest.NewRecorder(), nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 3*time.Second, time.Since(start))
	})
}

type disconnectAfterSaveRepo struct {
	mocks.MockIllRepositorySuccess
	disconnect context.CancelFunc
	savedID    string
}

func (r *disconnectAfterSaveRepo) SaveIllTransaction(_ common.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	r.savedID = params.ID
	r.disconnect()
	return ill_db.IllTransaction(params), nil
}

type requestNoticeRepo struct {
	events.EventRepo
	saved    events.Event
	deadline time.Time
}

func (r *requestNoticeRepo) WithTxFunc(ctx common.ExtendedContext, fn func(events.EventRepo) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.deadline, _ = ctx.Deadline()
	return fn(r)
}

func (r *requestNoticeRepo) SaveEvent(_ common.ExtendedContext, params events.SaveEventParams) (events.Event, error) {
	r.saved = events.Event(params)
	return r.saved, nil
}

func (*requestNoticeRepo) Notify(common.ExtendedContext, string, events.Signal, events.SignalTarget) error {
	return nil
}

func TestSavedRequestPublishesNoticeAfterDisconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		appCtx := common.CreateExtCtxWithArgs(t.Context(), nil)
		requestCtx, disconnect := context.WithCancel(t.Context())
		defer disconnect()
		illRepo := &disconnectAfterSaveRepo{disconnect: disconnect}
		eventRepo := &requestNoticeRepo{}
		bus := events.WithContext(events.NewPostgresEventBus(eventRepo, ""), appCtx)
		msg := iso18626.NewISO18626Message()
		msg.Request = &iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "request"}}

		handleRequest(common.CreateExtCtxWithArgs(requestCtx, nil), msg, httptest.NewRecorder(), illRepo, bus, nil, "")

		require.ErrorIs(t, requestCtx.Err(), context.Canceled)
		require.Equal(t, events.EventNameRequestReceived, eventRepo.saved.EventName)
		require.Equal(t, illRepo.savedID, eventRepo.saved.IllTransactionID)
		require.Equal(t, 5*time.Second, time.Until(eventRepo.deadline))
	})
}

type confirmationEventRepo struct {
	events.EventRepo
	byID map[string]events.Event
}

func (r confirmationEventRepo) GetEvent(_ common.ExtendedContext, id string) (events.Event, error) {
	event, ok := r.byID[id]
	if !ok {
		return events.Event{}, pgx.ErrNoRows
	}
	return event, nil
}

type inlineConfirmationBus struct{ events.EventBus }

func (inlineConfirmationBus) ProcessTask(ctx common.ExtendedContext, event events.Event, _ events.SignalTarget, fn func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	event.EventStatus, _ = fn(ctx, event)
	return event, nil
}

func TestConfirmSupplierMsgUsesItsOwnNotice(t *testing.T) {
	for _, parentID := range []string{"supplier-notice", "forwarded-message"} {
		t.Run(parentID, func(t *testing.T) {
			ctx := common.CreateExtCtxWithArgs(t.Context(), nil)
			msg := iso18626.NewISO18626Message()
			msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
				Header: iso18626.Header{RequestingAgencyRequestId: "current-request"},
			}
			failure := suppMsgOkConfirmation()
			failure.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus = iso18626.TypeMessageStatusERROR
			eventRepo := confirmationEventRepo{byID: map[string]events.Event{
				"supplier-notice": {
					ID: "supplier-notice", EventName: events.EventNameSupplierMsgReceived, ParentID: createPgText("outer-message"),
					EventData: events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: msg}},
				},
				"forwarded-message": {
					ID: "forwarded-message", EventName: events.EventNameMessageRequester, ParentID: createPgText("supplier-notice"),
					ResultData: events.EventResult{CommonEventData: events.CommonEventData{IncomingMessage: failure}},
				},
				"outer-message": {ID: "outer-message", EventName: events.EventNameMessageRequester, ParentID: createPgText("outer-notice")},
				"outer-notice":  {ID: "outer-notice", EventName: events.EventNameSupplierMsgReceived},
			}}
			h := CreateIso18626Handler(inlineConfirmationBus{events.WithContext(events.NewPostgresEventBus(eventRepo, ""), ctx)}, eventRepo, nil, nil)
			response := make(chan messageConfirmation, 1)
			waitingReqs.register("supplier-notice", RequestWait{response: response})
			defer waitingReqs.take("supplier-notice")

			h.ConfirmSupplierMsg(ctx, events.Event{EventName: events.EventNameConfirmSupplierMsg, ParentID: createPgText(parentID)})

			select {
			case confirmation := <-response:
				require.NotNil(t, confirmation.message)
				header := confirmation.message.SupplyingAgencyMessageConfirmation.ConfirmationHeader
				require.Equal(t, "current-request", header.RequestingAgencyRequestId)
				wantStatus := iso18626.TypeMessageStatusOK
				if parentID == "forwarded-message" {
					wantStatus = iso18626.TypeMessageStatusERROR
				}
				require.Equal(t, wantStatus, header.MessageStatus)
			default:
				t.Fatal("the supplier notice's waiter did not receive a confirmation")
			}
		})
	}
}
