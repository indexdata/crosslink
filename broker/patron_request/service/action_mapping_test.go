package prservice

import (
	"slices"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestNewReturnableActionMapping(t *testing.T) {
	borrowerStateActionMapping := map[pr_db.PatronRequestState][]PatronRequestAction{
		BorrowerStateNew:              {{actionName: BorrowerActionValidate, auto: true}},
		BorrowerStateValidated:        {{actionName: BorrowerActionSendRequest}},
		BorrowerStateSupplierLocated:  {{actionName: BorrowerActionCancelRequest}},
		BorrowerStateConditionPending: {{actionName: BorrowerActionAcceptCondition}, {actionName: BorrowerActionRejectCondition}},
		BorrowerStateWillSupply:       {{actionName: BorrowerActionCancelRequest}},
		BorrowerStateShipped:          {{actionName: BorrowerActionReceive}},
		BorrowerStateReceived:         {{actionName: BorrowerActionCheckOut}},
		BorrowerStateCheckedOut:       {{actionName: BorrowerActionCheckIn}},
		BorrowerStateCheckedIn:        {{actionName: BorrowerActionShipReturn}},
	}

	lenderStateActionMapping := map[pr_db.PatronRequestState][]PatronRequestAction{
		LenderStateNew:               {{actionName: LenderActionValidate, auto: true}},
		LenderStateValidated:         {{actionName: LenderActionWillSupply, auto: true}, {actionName: LenderActionCannotSupply}, {actionName: LenderActionAddCondition}},
		LenderStateWillSupply:        {{actionName: LenderActionAddCondition}, {actionName: LenderActionShip}, {actionName: LenderActionCannotSupply}},
		LenderStateConditionPending:  {{actionName: LenderActionAddCondition}, {actionName: LenderActionCannotSupply}},
		LenderStateConditionAccepted: {{actionName: LenderActionShip}, {actionName: LenderActionCannotSupply}},
		LenderStateShippedReturn:     {{actionName: LenderActionMarkReceived}},
		LenderStateCancelRequested:   {{actionName: LenderActionAcceptCancel}, {actionName: LenderActionRejectCancel}},
	}

	stateModel, err := LoadStateModelByName("returnables")
	assert.Nil(t, err)
	returnableActionMapping := NewActionMapping(stateModel)

	assert.NotNil(t, returnableActionMapping)

	mapCompare(t, returnableActionMapping.borrowerStateActionMapping, borrowerStateActionMapping)

	mapCompare(t, returnableActionMapping.lenderStateActionMapping, lenderStateActionMapping)
}

var actionMappingService = ActionMappingService{}

func mustActionMapping(t *testing.T) *ActionMapping {
	t.Helper()
	mapping, err := actionMappingService.GetActionMapping(pr_db.PatronRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, mapping)
	return mapping
}

func TestIsActionAvailable(t *testing.T) {
	mapping := mustActionMapping(t)
	// Borrower
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionValidate))
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionReceive))

	// Lender
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, LenderActionShip))
	assert.True(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending}, LenderActionAddCondition))
	assert.False(t, mapping.IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, BorrowerActionRejectCondition))
}

func TestGetActionsForPatronRequest(t *testing.T) {
	mapping := mustActionMapping(t)
	// Borrower
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionValidate}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew,
		LastAction:       pgtype.Text{String: string(BorrowerActionValidate), Valid: true},
		LastActionResult: pgtype.Text{String: string(events.EventStatusError), Valid: true},
	}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionValidate}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew,
		LastAction:       pgtype.Text{String: string(BorrowerActionValidate), Valid: true},
		LastActionResult: pgtype.Text{String: string(events.EventStatusProblem), Valid: true},
	}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew,
		LastAction:       pgtype.Text{String: string(BorrowerActionValidate), Valid: true},
		LastActionResult: pgtype.Text{String: string(events.EventStatusSuccess), Valid: true},
	}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCompleted}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCancelled}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionSendRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionCancelRequest}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateSupplierLocated}))
	listCompare(t, []pr_db.PatronRequestAction{BorrowerActionAcceptCondition, BorrowerActionRejectCondition}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateConditionPending}))

	// Lender
	listCompare(t, []pr_db.PatronRequestAction{LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}))
	listCompare(t, []pr_db.PatronRequestAction{LenderActionAddCondition, LenderActionCannotSupply}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending}))
	listCompare(t, []pr_db.PatronRequestAction{}, mapping.GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped}))
}

func TestGetAllowedActionsForPatronRequest1(t *testing.T) {
	mapping := mustActionMapping(t)
	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{}}, mapping.GetAllowedActionsForPatronRequest(
		pr_db.PatronRequest{
			Side: SideBorrowing, State: BorrowerStateNew}))

	tt := true
	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{{Name: string(BorrowerActionSendRequest), Parameters: []string{}, Primary: &tt}}},
		mapping.GetAllowedActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated}))

	assert.Equal(t, proapi.AllowedActions{Actions: []proapi.AllowedAction{
		{Name: string(LenderActionAddCondition), Parameters: []string{"note", "loanCondition", "cost", "currency"}},
		{Name: string(LenderActionShip), Parameters: []string{"note"}, Primary: &tt},
		{Name: string(LenderActionCannotSupply), Parameters: []string{"note", "reasonUnfilled"}},
	}}, mapping.GetAllowedActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}))
}

func TestGetActionTransitionMissingCases(t *testing.T) {
	mapping := mustActionMapping(t)

	// Supported action, but failure transition is not defined in state model	.
	_, ok := mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew},
		BorrowerActionValidate,
		ActionOutcomeFailure,
	)
	assert.False(t, ok)

	// Unsupported outcome key should not resolve any transition.
	_, ok = mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew},
		BorrowerActionValidate,
		"unknown-outcome",
	)
	assert.False(t, ok)

	// Action not configured for state should not resolve transition.
	_, ok = mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateValidated},
		BorrowerActionValidate,
		ActionOutcomeSuccess,
	)
	assert.False(t, ok)
}

func TestGetActionTransitionConditionPendingSelfTransition(t *testing.T) {
	mapping := mustActionMapping(t)

	transition, ok := mapping.GetActionTransition(
		pr_db.PatronRequest{Side: SideLending, State: LenderStateConditionPending},
		LenderActionAddCondition,
		ActionOutcomeSuccess,
	)
	assert.True(t, ok)
	assert.Equal(t, LenderStateConditionPending, transition)
}

func listCompare(t *testing.T, list1 []pr_db.PatronRequestAction, list2 []pr_db.PatronRequestAction) {
	assert.Equal(t, len(list1), len(list2))
	for i := range list1 {
		assert.True(t, slices.Contains(list2, list1[i]))
	}
}

func mapCompare(t *testing.T, map1 map[pr_db.PatronRequestState][]PatronRequestAction, map2 map[pr_db.PatronRequestState][]PatronRequestAction) {
	for stateName := range map1 {
		listOne := map1[stateName]
		listTwo := map2[stateName]
		assert.Equal(t, len(listOne), len(listTwo))
		for i := range listOne {
			assert.Equal(t, listOne[i].actionName, listTwo[i].actionName)
			assert.Equal(t, listOne[i].auto, listTwo[i].auto)
		}
	}
}
