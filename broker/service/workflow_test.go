package service

import (
	"context"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"testing"
)

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_EmptyMessageInfo(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	sam := iso18626.SupplyingAgencyMessage{}
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, sam, "1"))
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_NotCancelReason(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	sam := iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
		},
	}
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, sam, "1"))
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_NotAccepted(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	sam := iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
		},
	}
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, sam, "1"))
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ErrorReadingTransaction(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositoryError), WorkflowConfig{})
	yes := iso18626.TypeYesNoY
	sam := iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
			AnswerYesNo:      &yes,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled},
	}
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, sam, "1"))
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ErrorReadingEvent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositoryError), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, getCorrectSam(), "1"))
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_IncorrectEvent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryIncorrect), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, getCorrectSam(), "1"))
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ToBroker(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, getCorrectSam(), "2"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 1)
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ToSupplier(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	assert.False(t, manager.handleAndCheckCancelResponse(appCtx, getCorrectSam(), "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ToSupplierAnswerNoStatusCancel(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	no := iso18626.TypeYesNoN
	sam := getCorrectSam()
	sam.MessageInfo.AnswerYesNo = &no
	assert.False(t, manager.handleAndCheckCancelResponse(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ToSupplierAnswerYesStatusUnfilled(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	sam := getCorrectSam()
	sam.StatusInfo.Status = iso18626.TypeStatusUnfilled
	assert.False(t, manager.handleAndCheckCancelResponse(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestHandleCancelStatusAndCheckIfMessageRequesterNeeded_ToSupplierAnswerNoStatusUnfilled(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	no := iso18626.TypeYesNoN
	sam := getCorrectSam()
	sam.MessageInfo.AnswerYesNo = &no
	sam.StatusInfo.Status = iso18626.TypeStatusUnfilled
	assert.True(t, manager.handleAndCheckCancelResponse(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestOnMessageRequesterComplete(t *testing.T) {
	tests := []struct {
		name             string
		event            events.Event
		supMethodCalls   int
		tasksCreated     int
		broadcastCreated int
	}{
		{
			name:  "Empty event",
			event: events.Event{},
		},
		{
			name: "Not supplying message",
			event: events.Event{
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: &iso18626.ISO18626Message{},
					},
				},
			},
		},
		{
			name: "Supply message",
			event: events.Event{
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: &iso18626.ISO18626Message{
							SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{},
						},
					},
				},
			},
			broadcastCreated: 1,
		},
		{
			name: "Supply message unfilled",
			event: events.Event{
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: &iso18626.ISO18626Message{
							SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
								StatusInfo: iso18626.StatusInfo{
									Status: iso18626.TypeStatusUnfilled,
								},
							},
						},
					},
				},
			},
			broadcastCreated: 1,
			tasksCreated:     1,
		},
		{
			name: "Supply message cancel response not valid",
			event: events.Event{
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: &iso18626.ISO18626Message{
							SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
								MessageInfo: iso18626.MessageInfo{
									ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
								},
							},
						},
					},
				},
			},
			broadcastCreated: 1,
		},
		{
			name: "Supply message cancel response valid",
			event: events.Event{
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: &iso18626.ISO18626Message{
							SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
								MessageInfo: iso18626.MessageInfo{
									ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
								},
								StatusInfo: iso18626.StatusInfo{
									Status: iso18626.TypeStatusCancelled,
								},
							},
						},
					},
				},
			},
			broadcastCreated: 1,
			supMethodCalls:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
			eventBus := new(MockEventBus)
			mockIllRepo := new(MockIllRepositoryRequester)
			mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", mock.Anything, mock.Anything).Return()
			manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})

			manager.OnMessageRequesterComplete(appCtx, tt.event)

			mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", tt.supMethodCalls)
			assert.Equal(t, tt.broadcastCreated, eventBus.BroadcastCreated)
			assert.Equal(t, tt.tasksCreated, eventBus.TasksCreated)
		})
	}
}

func getCorrectSam() iso18626.SupplyingAgencyMessage {
	yes := iso18626.TypeYesNoY
	return iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
			AnswerYesNo:      &yes,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled},
	}
}

type MockEventRepositoryIncorrect struct {
	mocks.MockEventRepositorySuccess
}

func (r *MockEventRepositoryIncorrect) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	return events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: &iso18626.ISO18626Message{
					SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{},
				},
			},
		},
	}, nil
}

type MockEventRepositoryCorrect struct {
	mocks.MockEventRepositorySuccess
}

func (r *MockEventRepositoryCorrect) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	supId := "SUP1"
	if illTransId == "2" {
		supId = "BROKER"
	}
	return events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: &iso18626.ISO18626Message{
					RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
						Header: iso18626.Header{
							SupplyingAgencyId: iso18626.TypeAgencyId{
								AgencyIdType: iso18626.TypeSchemeValuePair{
									Text: "ISIL",
								},
								AgencyIdValue: supId,
							},
						},
					},
				},
			},
		},
	}, nil
}

type MockEventBus struct {
	events.PostgresEventBus
	TasksCreated     int
	BroadcastCreated int
}

func (r *MockEventBus) CreateTask(illTransactionID string, eventName events.EventName, data events.EventData, parentId *string) (string, error) {
	r.TasksCreated++
	return "id1", nil
}
func (r *MockEventBus) CreateTaskBroadcast(illTransactionID string, eventName events.EventName, data events.EventData, parentId *string) (string, error) {
	r.BroadcastCreated++
	return "id2", nil
}

type MockIllRepositoryRequester struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockIllRepositoryRequester) GetRequesterByIllTransactionId(ctx common.ExtendedContext, illTransactionId string) (ill_db.Peer, error) {
	mode := string(common.BrokerModeTransparent)
	if illTransactionId == "1" {
		mode = string(common.BrokerModeOpaque)
	}
	return ill_db.Peer{
		BrokerMode: mode,
	}, nil
}

func (r *MockIllRepositoryRequester) GetLocatedSuppliersByIllTransactionAndStatus(ctx common.ExtendedContext, params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	r.Called(ctx, params)
	return []ill_db.LocatedSupplier{{
		ID:               uuid.New().String(),
		IllTransactionID: params.IllTransactionID,
		SupplierStatus:   params.SupplierStatus,
		SupplierID:       uuid.New().String(),
	}}, nil
}
