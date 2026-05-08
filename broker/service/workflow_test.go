package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestShouldForwardSAM_EmptyMessageInfo(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	sam := iso18626.SupplyingAgencyMessage{}
	assert.True(t, manager.shouldForwardSAM(appCtx, sam, "1"))
}

func TestShouldForwardSAM_NotCancelReason(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	sam := iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
		},
	}
	assert.True(t, manager.shouldForwardSAM(appCtx, sam, "1"))
}

func TestShouldForwardSAM_CancelResponseNotAccepted(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositorySuccess), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	sam := iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
		},
	}
	assert.True(t, manager.shouldForwardSAM(appCtx, sam, "1"))
}

func TestShouldForwardSAM_LatestRequesterCancelNotFound(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryNoRequesterCancel), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	yes := iso18626.TypeYesNoY
	sam := iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
			AnswerYesNo:      &yes,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled},
	}
	assert.True(t, manager.shouldForwardSAM(appCtx, sam, "1"))
}

func TestShouldForwardSAM_ErrorReadingLatestRequesterCancelEvent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(mocks.MockEventRepositoryError), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	assert.True(t, manager.shouldForwardSAM(appCtx, getCorrectSam(), "1"))
}

func TestShouldForwardSAM_LatestRequesterCancelMissingMessage(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryIncorrect), "")
	manager := CreateWorkflowManager(eventBus, new(mocks.MockIllRepositorySuccess), WorkflowConfig{})
	assert.True(t, manager.shouldForwardSAM(appCtx, getCorrectSam(), "1"))
}

func TestShouldForwardSAM_AcceptedResponseToBrokerCancel(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	assert.True(t, manager.shouldForwardSAM(appCtx, getCorrectSam(), "2"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestShouldForwardSAM_AcceptedResponseToSupplierCancel(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	assert.False(t, manager.shouldForwardSAM(appCtx, getCorrectSam(), "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestShouldForwardSAM_AcceptedResponseFromDifferentSupplier(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	sam := getCorrectSam()
	sam.Header.SupplyingAgencyId.AgencyIdValue = "SUP2"
	assert.True(t, manager.shouldForwardSAM(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestShouldForwardSAM_AcceptedResponseToSupplierCancelByStatus(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	no := iso18626.TypeYesNoN
	sam := getCorrectSam()
	sam.MessageInfo.AnswerYesNo = &no
	assert.False(t, manager.shouldForwardSAM(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestShouldForwardSAM_AcceptedResponseToSupplierCancelByAnswer(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	sam := getCorrectSam()
	sam.StatusInfo.Status = iso18626.TypeStatusUnfilled
	assert.False(t, manager.shouldForwardSAM(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestShouldForwardSAM_RejectedResponseToSupplierCancel(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := events.NewPostgresEventBus(new(MockEventRepositoryCorrect), "")
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", appCtx, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})
	no := iso18626.TypeYesNoN
	sam := getCorrectSam()
	sam.MessageInfo.AnswerYesNo = &no
	sam.StatusInfo.Status = iso18626.TypeStatusUnfilled
	assert.True(t, manager.shouldForwardSAM(appCtx, sam, "3"))
	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
}

func TestRequesterMessageReceived_BrokerCancelSkipsNewSuppliers(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := new(MockEventBus)
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", mock.Anything,
		mock.MatchedBy(func(params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) bool {
			return params.IllTransactionID == "1" && params.SupplierStatus == ill_db.SupplierStateNewPg
		})).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})

	var message = iso18626.NewISO18626Message()
	message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Action: iso18626.TypeActionCancel,
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "BROKER",
			},
		},
	}
	manager.RequesterMessageReceived(appCtx, events.Event{
		IllTransactionID: "1", // opaque mode in MockIllRepositoryRequester
		EventStatus:      events.EventStatusSuccess,
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: message,
			},
		},
	})

	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 1)
	assert.Equal(t, 1, eventBus.TasksCreated)
}

func TestRequesterMessageReceived_SupplierCancelDoesNotSkipNewSuppliers(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventBus := new(MockEventBus)
	mockIllRepo := new(MockIllRepositoryRequester)
	mockIllRepo.On("GetLocatedSuppliersByIllTransactionAndStatus", mock.Anything, mock.Anything).Return()
	manager := CreateWorkflowManager(eventBus, mockIllRepo, WorkflowConfig{})

	var message = iso18626.NewISO18626Message()
	message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Action: iso18626.TypeActionCancel,
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP1",
			},
		},
	}
	manager.RequesterMessageReceived(appCtx, events.Event{
		IllTransactionID: "1", // opaque mode in MockIllRepositoryRequester
		EventStatus:      events.EventStatusSuccess,
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: message,
			},
		},
	})

	mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", 0)
	assert.Equal(t, 1, eventBus.TasksCreated)
}
func messageFromSam(sam *iso18626.SupplyingAgencyMessage) *iso18626.ISO18626Message {
	var message = iso18626.NewISO18626Message()
	message.SupplyingAgencyMessage = sam
	return message
}

func TestOnMessageRequesterComplete(t *testing.T) {
	tests := []struct {
		name             string
		event            events.Event
		supplierStatuses []string
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
						IncomingMessage: messageFromSam(nil),
					},
				},
			},
		},
		{
			name: "Supply message",
			event: events.Event{
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{}),
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
						IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{
							StatusInfo: iso18626.StatusInfo{
								Status: iso18626.TypeStatusUnfilled,
							},
						}),
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
						IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{
							MessageInfo: iso18626.MessageInfo{
								ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
							},
						}),
					},
				},
			},
			broadcastCreated: 1,
		},
		{
			name: "Supply message broker cancel response valid",
			event: events.Event{
				IllTransactionID: "2",
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{
							MessageInfo: iso18626.MessageInfo{
								ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
							},
							StatusInfo: iso18626.StatusInfo{
								Status: iso18626.TypeStatusCancelled,
							},
						}),
					},
				},
			},
			broadcastCreated: 1,
			supplierStatuses: []string{ill_db.SupplierStateSelectedPg.String},
		},
		{
			name: "Supply message supplier cancel response valid",
			event: events.Event{
				IllTransactionID: "3",
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{
							MessageInfo: iso18626.MessageInfo{
								ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
							},
							StatusInfo: iso18626.StatusInfo{
								Status: iso18626.TypeStatusCancelled,
							},
						}),
					},
				},
			},
			broadcastCreated: 1,
		},
		{
			name: "Supply message unsolicited supplier cancel",
			event: events.Event{
				IllTransactionID: "unsolicited-cancel",
				EventData: events.EventData{
					CommonEventData: events.CommonEventData{
						IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{
							MessageInfo: iso18626.MessageInfo{
								ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
							},
							StatusInfo: iso18626.StatusInfo{
								Status: iso18626.TypeStatusCancelled,
							},
						}),
					},
				},
			},
			broadcastCreated: 1,
			supplierStatuses: []string{
				ill_db.SupplierStateSelectedPg.String,
				ill_db.SupplierStateNewPg.String,
			},
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

			mockIllRepo.AssertNumberOfCalls(t, "GetLocatedSuppliersByIllTransactionAndStatus", len(tt.supplierStatuses))
			for _, supplierStatus := range tt.supplierStatuses {
				mockIllRepo.AssertCalled(t, "GetLocatedSuppliersByIllTransactionAndStatus", mock.Anything,
					mock.MatchedBy(func(params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) bool {
						return params.IllTransactionID == tt.event.IllTransactionID &&
							params.SupplierStatus.String == supplierStatus
					}))
			}
			assert.Equal(t, tt.broadcastCreated, eventBus.BroadcastCreated)
			assert.Equal(t, tt.tasksCreated, eventBus.TasksCreated)
		})
	}
}

func getCorrectSam() iso18626.SupplyingAgencyMessage {
	yes := iso18626.TypeYesNoY
	return iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "SUP1",
			},
		},
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
				IncomingMessage: messageFromSam(&iso18626.SupplyingAgencyMessage{}),
			},
		},
	}, nil
}

type MockEventRepositoryCorrect struct {
	mocks.MockEventRepositorySuccess
}

type MockEventRepositoryNoRequesterCancel struct {
	mocks.MockEventRepositorySuccess
}

func (r *MockEventRepositoryNoRequesterCancel) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	return events.Event{}, pgx.ErrNoRows
}

func (r *MockEventRepositoryCorrect) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	supId := "SUP1"
	if illTransId == "2" {
		supId = "BROKER"
	}
	var message = iso18626.NewISO18626Message()
	message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: supId,
			},
		},
	}
	return events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: message,
			},
		},
	}, nil
}

type MockEventBus struct {
	events.PostgresEventBus
	TasksCreated     int
	BroadcastCreated int
}

func (r *MockEventBus) CreateTask(illTransactionID string, eventName events.EventName, data events.EventData, eventClass events.EventDomain, parentId *string, target events.SignalTarget) (string, error) {
	if target == events.SignalObservers {
		r.BroadcastCreated++
		return "id2", nil
	}
	r.TasksCreated++
	if target == events.SignalAll {
		r.BroadcastCreated++
		return "id2", nil
	}
	return "id1", nil
}

func (r *MockEventBus) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	return (&MockEventRepositoryCorrect{}).GetLatestRequestEventByAction(ctx, illTransId, action)
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
