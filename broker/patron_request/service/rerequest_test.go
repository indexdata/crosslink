package prservice

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRerequest(t *testing.T) {
	for _, state := range []pr_db.PatronRequestState{BorrowerStateCancelled, BorrowerStateUnfilled} {
		t.Run(string(state), func(t *testing.T) {
			repo := new(MockPrRepo)
			bus := new(MockEventBus)
			lmsCreator := new(MockLmsCreator)
			lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(createLmsAdapterMockLog(), nil)
			bus.On("ProcessExclusiveTask", "REQ1-2-task-1").Return(events.Event{EventStatus: events.EventStatusSuccess}, nil)
			repo.On("GetPatronRequestById", "REQ1-2").Return(pr_db.PatronRequest{State: BorrowerStateValidated}, nil)
			service := CreatePatronRequestActionService(repo, new(MockIllRepo), bus, new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
			requestType := iso18626.TypeRequestTypeRetry
			original := pr_db.PatronRequest{
				ID: "REQ1-1", Side: SideBorrowing, State: state, TerminalState: true,
				RequesterSymbol: getDbText("ISIL:REQ1"), SupplierSymbol: getDbText("ISIL:SUP1"),
				Patron: getDbText("patron"), Tenant: getDbText("tenant"), StateModel: "default",
				IllRequest: iso18626.Request{
					Header:            iso18626.Header{RequestingAgencyRequestId: "REQ1-1", SupplyingAgencyRequestId: "old-supplier-id"},
					BibliographicInfo: iso18626.BibliographicInfo{Title: "Original title"},
					ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan, RequestType: &requestType, RequestingAgencyPreviousRequestId: "older-request"},
				},
				RetryBibInfo: &iso18626.BibliographicInfo{Title: "Retry correction"},
			}
			repo.On("GetPatronRequestById", original.ID).Return(original, nil)
			repo.On("GetPatronRequestByIdForUpdate", original.ID).Return(original, nil)
			action := BorrowerActionRerequest
			status, result := service.handleInvokeAction(appCtx, events.Event{
				ID: "rerequest-event", PatronRequestID: original.ID,
				EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
			})
			require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
			next := repo.createdPr
			require.Equal(t, "REQ1-2", next.ID)
			assert.Equal(t, original.ID, next.PrevReqID.String)
			assert.Equal(t, next.ID, repo.savedPr.NextReqID.String)
			assert.Equal(t, state, repo.savedPr.State)
			assert.True(t, repo.savedPr.TerminalState)
			assert.Equal(t, BorrowerStateNew, next.State)
			assert.False(t, next.TerminalState)
			assert.Equal(t, original.Patron, next.Patron)
			assert.Equal(t, original.Tenant, next.Tenant)
			assert.Equal(t, original.RequesterPickupLocationID, next.RequesterPickupLocationID)
			assert.False(t, next.SupplierSymbol.Valid)
			assert.Empty(t, next.SupplierSymbol.String)
			assert.Equal(t, original.IllRequest.BibliographicInfo.Title, next.IllRequest.BibliographicInfo.Title)
			assert.Empty(t, next.Items)
			assert.Nil(t, next.RetryBibInfo)
			assert.Equal(t, iso18626.TypeRequestTypeNew, *next.IllRequest.ServiceInfo.RequestType)
			assert.Empty(t, next.IllRequest.ServiceInfo.RequestingAgencyPreviousRequestId)
			assert.Empty(t, next.IllRequest.Header.SupplyingAgencyRequestId)
			assert.Equal(t, iso18626.TypeRequestTypeRetry, *original.IllRequest.ServiceInfo.RequestType)
			assert.NotEmpty(t, bus.createdTaskData)

			status, sent, err := service.messageSender.sendBorrowingRequest(appCtx, "send", next, next.IllRequest)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			outgoing := sent.OutgoingMessage.Request
			assert.Equal(t, iso18626.TypeRequestTypeNew, *outgoing.ServiceInfo.RequestType)
			assert.Empty(t, outgoing.ServiceInfo.RequestingAgencyPreviousRequestId)
			assert.Equal(t, next.ID, outgoing.Header.RequestingAgencyRequestId)
		})
	}
}

func TestRerequestAvailability(t *testing.T) {
	mapping := mustActionMapping(t)
	for _, state := range []pr_db.PatronRequestState{BorrowerStateCancelled, BorrowerStateUnfilled, BorrowerStateNew, BorrowerStateRetryPending, BorrowerStateRetryAccepted} {
		for _, side := range []pr_db.PatronRequestSide{SideBorrowing, SideLending} {
			pr := pr_db.PatronRequest{State: state, Side: side}
			expected := side == SideBorrowing && (state == BorrowerStateCancelled || state == BorrowerStateUnfilled)
			assert.Equal(t, expected, mapping.IsActionAvailable(pr, BorrowerActionRerequest))
			pr.NextReqID = getDbText("successor")
			assert.False(t, mapping.IsActionAvailable(pr, BorrowerActionRerequest))
		}
	}
}

func TestSendLinkedRetryRequest(t *testing.T) {
	sender := PatronRequestMessageSender{eventBus: new(MockEventBus), iso18626Handler: new(MockIso18626Handler)}
	requestType := iso18626.TypeRequestTypeRetry
	request := iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{RequestType: &requestType}}
	status, result, err := sender.sendBorrowingRequest(appCtx, "send", pr_db.PatronRequest{
		ID: "successor", RequesterSymbol: getDbText("ISIL:REQ1"), PrevReqID: getDbText("previous"),
	}, request)
	require.NoError(t, err)
	require.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeRequestTypeRetry, *result.OutgoingMessage.Request.ServiceInfo.RequestType)
	assert.Equal(t, "previous", result.OutgoingMessage.Request.ServiceInfo.RequestingAgencyPreviousRequestId)
	assert.Equal(t, iso18626.TypeRequestTypeRetry, *request.ServiceInfo.RequestType)
}

func TestRerequestDoesNotReplaceSuccessor(t *testing.T) {
	service := CreatePatronRequestActionService(new(MockPrRepo), nil, nil, nil, nil, nil, nil, nil)
	original := pr_db.PatronRequest{ID: "original", State: BorrowerStateCancelled, NextReqID: getDbText("existing")}
	result := service.createSuccessorBorrowingRequest(appCtx, original, false)
	assert.Equal(t, events.EventStatusError, result.status)
	assert.Empty(t, result.successorPr.ID)
	assert.Equal(t, "existing", result.pr.NextReqID.String)
}

func TestLegacyRerequestStartsInitialWorkflow(t *testing.T) {
	for _, state := range []pr_db.PatronRequestState{BorrowerStateCancelled, BorrowerStateUnfilled} {
		t.Run(string(state), func(t *testing.T) {
			repo := new(MockPrRepo)
			bus := new(MockEventBus)
			bus.On("ProcessExclusiveTask", "REQ1-2-task-1").Return(events.Event{EventStatus: events.EventStatusSuccess}, nil).Once()
			repo.On("GetPatronRequestById", "REQ1-2").Return(pr_db.PatronRequest{State: BorrowerStateValidated}, nil).Once()
			original := pr_db.PatronRequest{
				ID: "REQ1-1", Side: SideBorrowing, State: state, TerminalState: true,
				RequesterSymbol: getDbText("ISIL:REQ1"),
			}
			service := CreatePatronRequestActionService(repo, nil, bus, new(MockIso18626Handler), nil, nil, nil, nil)
			result := service.createSuccessorBorrowingRequest(appCtx, original, false)
			require.Equal(t, events.EventStatusSuccess, result.status)
			next := result.successorPr
			assert.Equal(t, iso18626.TypeServiceTypeLoan, next.IllRequest.ServiceInfo.ServiceType)
			require.NoError(t, service.RunAutoActionsOnStateEntry(appCtx, next, nil, ""))
			require.Len(t, bus.createdTaskData, 1)
			assert.Equal(t, BorrowerActionValidatePatron, *bus.createdTaskData[0].Action)
			bus.AssertExpectations(t)
			repo.AssertExpectations(t)

			status, sent, err := service.messageSender.sendBorrowingRequest(appCtx, "send", next, next.IllRequest)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, iso18626.TypeRequestTypeNew, *sent.OutgoingMessage.Request.ServiceInfo.RequestType)
			assert.Empty(t, sent.OutgoingMessage.Request.ServiceInfo.RequestingAgencyPreviousRequestId)
			assert.Equal(t, iso18626.TypeServiceTypeLoan, next.IllRequest.ServiceInfo.ServiceType)
		})
	}
}

func TestSuccessorPersistsRequestType(t *testing.T) {
	for _, retry := range []bool{false, true} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("retry=%t/legacy=%t", retry, legacy), func(t *testing.T) {
				repo := new(MockPrRepo)
				service := CreatePatronRequestActionService(repo, nil, new(MockEventBus), new(MockIso18626Handler), nil, nil, nil, nil)
				inheritedType := iso18626.TypeRequestTypeRetry
				if retry {
					inheritedType = iso18626.TypeRequestTypeNew
				}
				original := pr_db.PatronRequest{
					ID: "REQ1-1", Side: SideBorrowing, State: "CUSTOM_STATE",
					RequesterSymbol: getDbText("ISIL:REQ1"),
					SupplierSymbol:  getDbText("ISIL:SUP1"),
				}
				if !legacy {
					original.IllRequest.ServiceInfo = &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan, RequestType: &inheritedType}
				}
				result := service.createSuccessorBorrowingRequest(appCtx, original, retry)
				require.Equal(t, events.EventStatusSuccess, result.status)
				next := result.successorPr
				expectedType := iso18626.TypeRequestTypeNew
				expectedPrevious := ""
				if retry {
					expectedType = iso18626.TypeRequestTypeRetry
					expectedPrevious = original.ID
					assert.Equal(t, original.SupplierSymbol, next.SupplierSymbol)
				} else {
					assert.False(t, next.SupplierSymbol.Valid)
					assert.Empty(t, next.SupplierSymbol.String)
				}
				require.NotNil(t, next.IllRequest.ServiceInfo.RequestType)
				assert.Equal(t, expectedType, *next.IllRequest.ServiceInfo.RequestType)
				assert.Equal(t, expectedPrevious, next.IllRequest.ServiceInfo.RequestingAgencyPreviousRequestId)
				// Round-trip the persisted ISO request before sending without a predecessor.
				data, err := json.Marshal(next.IllRequest)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(data, &next.IllRequest))
				status, sent, err := service.messageSender.sendBorrowingRequest(appCtx, "send", next, next.IllRequest)
				require.NoError(t, err)
				require.Equal(t, events.EventStatusSuccess, status)
				assert.Equal(t, expectedType, *sent.OutgoingMessage.Request.ServiceInfo.RequestType)
				assert.Equal(t, expectedPrevious, sent.OutgoingMessage.Request.ServiceInfo.RequestingAgencyPreviousRequestId)
				repo.AssertNotCalled(t, "GetPatronRequestById", original.ID)
			})
		}
	}
}

func TestSendRequestDuplicateCheckBypass(t *testing.T) {
	for _, linked := range []bool{false, true} {
		for _, requestType := range []iso18626.TypeRequestType{iso18626.TypeRequestTypeNew, iso18626.TypeRequestTypeRetry} {
			t.Run(fmt.Sprintf("linked=%t/type=%s", linked, requestType), func(t *testing.T) {
				isoHandler := new(MockIso18626Handler)
				sender := PatronRequestMessageSender{eventBus: new(MockEventBus), iso18626Handler: isoHandler}
				pr := pr_db.PatronRequest{ID: "successor", RequesterSymbol: getDbText("ISIL:REQ1")}
				if linked {
					pr.PrevReqID = getDbText("previous")
				}
				request := iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{RequestType: &requestType}}
				status, result, err := sender.sendBorrowingRequest(appCtx, "send", pr, request)
				require.NoError(t, err)
				require.Equal(t, events.EventStatusSuccess, status)
				require.Len(t, isoHandler.requestOptions, 1)
				assert.Equal(t, linked && requestType == iso18626.TypeRequestTypeNew, isoHandler.requestOptions[0].SkipDuplicateCheck)
				assert.Equal(t, requestType, *result.OutgoingMessage.Request.ServiceInfo.RequestType)
			})
		}
	}
}
