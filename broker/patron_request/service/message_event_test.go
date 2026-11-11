package prservice

import (
	"encoding/json"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierMessageStateModelEvent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		state        pr_db.PatronRequestState
		status       iso18626.TypeStatus
		notification bool
		local        bool
		updateFails  bool
		wantEvent    MessageEvent
		wantState    pr_db.PatronRequestState
		wantProblem  bool
	}{
		{name: "local loan completion", state: BorrowerStateLocalSupply, status: iso18626.TypeStatusLoanCompleted, local: true, wantEvent: SupplierCompletedLocal, wantState: BorrowerStateLocalSupply},
		{name: "local copy completion", state: BorrowerStateLocalSupply, status: iso18626.TypeStatusCopyCompleted, local: true, wantEvent: SupplierCompletedLocal, wantState: BorrowerStateLocalSupply},
		{name: "local cancellation", state: BorrowerStateLocalSupply, status: iso18626.TypeStatusCancelled, local: true, wantEvent: SupplierCancelledLocal, wantState: BorrowerStateLocalSupply},
		{name: "local unfilled", state: BorrowerStateLocalSupply, status: iso18626.TypeStatusUnfilled, local: true, wantEvent: SupplierUnfilledLocal, wantState: BorrowerStateLocalSupply},
		{name: "supplier transition", state: BorrowerStateSent, status: iso18626.TypeStatusWillSupply, wantEvent: SupplierWillSupply, wantState: BorrowerStateWillSupply},
		{name: "rejected event", state: BorrowerStateCompleted, status: iso18626.TypeStatusWillSupply, wantEvent: SupplierWillSupply, wantProblem: true},
		{name: "request update fails", state: BorrowerStateSent, status: iso18626.TypeStatusWillSupply, updateFails: true, wantEvent: SupplierWillSupply, wantProblem: true},
		{name: "notification does not classify status", state: BorrowerStateSent, status: iso18626.TypeStatusWillSupply, notification: true},
		{name: "unrecognized status", state: BorrowerStateSent, status: iso18626.TypeStatus("unknown"), wantProblem: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "supplier-message"
			if tc.updateFails {
				id += "-error"
			}
			supplier := "ISIL:SUP"
			if tc.local {
				supplier = "ISIL:REQ"
			}
			pr := pr_db.PatronRequest{
				ID: id, Side: SideBorrowing, State: tc.state,
				RequesterSymbol: getDbText("ISIL:REQ"), SupplierSymbol: getDbText(supplier),
			}
			prRepo := new(MockPrRepo)
			prRepo.On("GetPatronRequestByIdAndSide", id, SideBorrowing).Return(pr, nil)
			bus := new(MockEventBus)
			handler := CreatePatronRequestMessageHandler(prRepo, nil, nil, bus)
			message := iso18626.NewISO18626Message()
			message.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
				Header: iso18626.Header{
					RequestingAgencyRequestId: id,
					SupplyingAgencyId: iso18626.TypeAgencyId{
						AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
						AgencyIdValue: supplier[len("ISIL:"):],
					},
				},
				MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange},
				StatusInfo:  iso18626.StatusInfo{Status: tc.status},
			}
			if tc.notification {
				message.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageNotification
			}

			response, err := handler.HandleMessage(appCtx, message, nil)
			if tc.wantProblem {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assertMessageStateModelEvent(t, bus, message, response, tc.wantEvent, tc.wantProblem)
			if tc.wantState != "" {
				assert.Equal(t, tc.wantState, prRepo.savedPr.State)
			}
		})
	}
}

func TestRequesterMessageStateModelEvent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		state       pr_db.PatronRequestState
		action      iso18626.TypeAction
		wantEvent   MessageEvent
		wantState   pr_db.PatronRequestState
		wantProblem bool
	}{
		{name: "requester transition", state: LenderStateShipped, action: iso18626.TypeActionReceived, wantEvent: RequesterReceived, wantState: LenderStateReceived},
		{name: "rejected event", state: LenderStateNew, action: iso18626.TypeActionReceived, wantEvent: RequesterReceived, wantProblem: true},
		{name: "notification", state: LenderStateShipped, action: iso18626.TypeActionNotification},
		{name: "unrecognized action", state: LenderStateShipped, action: iso18626.TypeAction("unknown"), wantProblem: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := pr_db.PatronRequest{ID: "requester-message", Side: SideLending, State: tc.state}
			prRepo := new(MockPrRepo)
			prRepo.On("GetPatronRequestByIdAndSide", pr.ID, SideLending).Return(pr, nil)
			bus := new(MockEventBus)
			handler := CreatePatronRequestMessageHandler(prRepo, nil, nil, bus)
			message := iso18626.NewISO18626Message()
			message.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
				Header: iso18626.Header{SupplyingAgencyRequestId: pr.ID},
				Action: tc.action,
			}

			response, err := handler.HandleMessage(appCtx, message, nil)
			if tc.wantProblem {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assertMessageStateModelEvent(t, bus, message, response, tc.wantEvent, tc.wantProblem)
			if tc.wantState != "" {
				assert.Equal(t, tc.wantState, prRepo.savedPr.State)
			}
		})
	}
}

func assertMessageStateModelEvent(t *testing.T, bus *MockEventBus, incoming, response *iso18626.ISO18626Message, want MessageEvent, wantProblem bool) {
	t.Helper()
	require.Len(t, bus.processedTaskEvents, 1)
	event := bus.processedTaskEvents[0]
	assert.Equal(t, events.EventNamePatronRequestMessage, event.EventName)
	assert.Equal(t, string(want), event.ResultData.StateModelEvent)
	assert.Same(t, incoming, event.ResultData.IncomingMessage)
	require.NotNil(t, response)
	assert.Same(t, response, event.ResultData.OutgoingMessage)
	if wantProblem {
		assert.Equal(t, events.EventStatusProblem, event.EventStatus)
		assert.NotNil(t, event.ResultData.EventError)
	} else {
		assert.Equal(t, events.EventStatusSuccess, event.EventStatus)
		assert.Nil(t, event.ResultData.EventError)
	}
	data, err := json.Marshal(event.ResultData)
	require.NoError(t, err)
	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &result))
	if want == "" {
		assert.NotContains(t, result, "stateModelEvent")
	} else {
		assert.JSONEq(t, `"`+string(want)+`"`, string(result["stateModelEvent"]))
	}
}
