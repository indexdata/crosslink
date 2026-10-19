package sched_service

import (
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverdueDefaultFiltersStatesInDatabase(t *testing.T) {
	for _, defaults := range prservice.GetStateModelBatchActionDefaults() {
		if defaults.ActionName != "overdue" {
			continue
		}
		require.Equal(t, "side = lending and (state = RECEIVED or state = RENEWED)", defaults.BatchQuery)
		repo := new(mockEmailPrRepo)
		svc := NewBatchActionService(new(mockBatchActionEventBus), repo, new(mockBatchActionCleanupRepo), nil)
		event := batchActionEvent("overdue")
		event.EventData.BatchActionData.Selector = defaults.BatchQuery
		status, result := svc.batchAction(testCtx, event)
		require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
		assert.Contains(t, repo.gotQuery.GetWhereClause(), "state =")
		assert.Contains(t, repo.gotQuery.GetWhereClause(), "due_at <")
		return
	}
	t.Fatal("overdue batch default not found")
}

func TestOverdueBatchPreservesSelectorSortingAndGrouping(t *testing.T) {
	repo := new(mockEmailPrRepo)
	svc := NewBatchActionService(new(mockBatchActionEventBus), repo, new(mockBatchActionCleanupRepo), nil)
	event := batchActionEvent("overdue")
	event.EventData.BatchActionData.Owner = ""
	selector := "state = RECEIVED or state = RENEWED sortBy due_at/sort.ascending"
	event.EventData.BatchActionData.Selector = selector
	status, result := svc.batchAction(testCtx, event)
	require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
	assert.Contains(t, repo.gotQuery.GetWhereClause(), "(state = $3 OR state = $4)")
	assert.Contains(t, repo.gotQuery.GetWhereClause(), "due_at <")
	assert.Contains(t, repo.gotQuery.GetOrderByClause(), "due_at")
	assert.Equal(t, selector, event.EventData.BatchActionData.Selector)
}

func TestOverdueBatchRetriesNeedsAttentionAndUsesNormalActions(t *testing.T) {
	repo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{{ID: "loan", Side: prservice.SideLending, State: prservice.LenderStateReceived, NeedsAttention: true}}}
	bus := new(mockBatchActionEventBus)
	svc := NewBatchActionService(bus, repo, new(mockBatchActionCleanupRepo), nil)
	event := batchActionEvent("overdue")
	for i := 0; i < 2; i++ {
		status, _ := svc.batchAction(testCtx, event)
		require.Equal(t, events.EventStatusSuccess, status)
	}
	require.Len(t, bus.createTaskCalls, 2)
	assert.Equal(t, events.EventNameInvokeBackgroundAction, bus.createTaskCalls[0].eventName)
	assert.Equal(t, prservice.LenderActionOverdue, *bus.createTaskCalls[0].data.Action)
	assert.Contains(t, repo.gotQuery.GetWhereClause(), "due_at <")
	assert.NotContains(t, repo.gotQuery.GetWhereClause(), "needs_attention")
	assert.NotContains(t, repo.gotQuery.GetWhereClause(), "state")
	assert.Contains(t, repo.gotQuery.GetWhereClause(), "supplier_symbol")
}

func TestOverdueBatchUsesStateModelAvailability(t *testing.T) {
	loan := iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}
	repo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{
		{ID: "custom", Side: prservice.SideLending, State: "CUSTOM_RECEIVED", IllRequest: loan},
		{ID: "unsupported", Side: prservice.SideLending, State: prservice.LenderStateShipped, IllRequest: loan},
		{ID: "already-overdue", Side: prservice.SideLending, State: prservice.LenderStateOverdue, IllRequest: loan},
		{ID: "pending", Side: prservice.SideLending, State: prservice.LenderStateRenewalPending, IllRequest: loan},
	}}
	bus := new(mockBatchActionEventBus)
	svc := NewBatchActionService(bus, repo, new(mockBatchActionCleanupRepo), nil)
	model, err := svc.actionMappingService.GetStateModel("default")
	require.NoError(t, err)
	for _, state := range model.States {
		if state.Name == string(prservice.LenderStateReceived) && state.Side == "SUPPLIER" {
			state.Name = "CUSTOM_RECEIVED"
			model.States = append(model.States, state)
			break
		}
	}
	status, result := svc.batchAction(testCtx, batchActionEvent("overdue"))
	require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
	require.Len(t, bus.createTaskCalls, 1)
	assert.Equal(t, "custom", bus.createTaskCalls[0].id)
	assert.Equal(t, "queued overdue actions for 1 loans", result.Note)
	assert.NotContains(t, repo.gotQuery.GetWhereClause(), "state")
}
