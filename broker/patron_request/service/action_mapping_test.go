package prservice

import (
	"slices"
	"testing"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/assert"
)

func TestNewReturnableActionMapping(t *testing.T) {
	borrowerStateActionMapping := map[pr_db.PatronRequestState][]pr_db.PatronRequestAction{
		BorrowerStateValidated:        {BorrowerActionSendRequest},
		BorrowerStateSupplierLocated:  {BorrowerActionCancelRequest},
		BorrowerStateConditionPending: {BorrowerActionAcceptCondition, BorrowerActionRejectCondition},
		BorrowerStateWillSupply:       {BorrowerActionCancelRequest},
		BorrowerStateShipped:          {BorrowerActionReceive},
		BorrowerStateReceived:         {BorrowerActionCheckOut},
		BorrowerStateCheckedOut:       {BorrowerActionCheckIn},
		BorrowerStateCheckedIn:        {BorrowerActionShipReturn},
	}

	lenderStateActionMapping := map[pr_db.PatronRequestState][]pr_db.PatronRequestAction{
		LenderStateValidated:         {LenderActionCannotSupply, LenderActionAddCondition},
		LenderStateWillSupply:        {LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip},
		LenderStateConditionPending:  {LenderActionCannotSupply},
		LenderStateConditionAccepted: {LenderActionShip, LenderActionCannotSupply},
		LenderStateShippedReturn:     {LenderActionMarkReceived},
		LenderStateCancelRequested:   {LenderActionMarkCancelled, LenderActionWillSupply},
	}

	stateModel, err := LoadStateModelByName("returnables")
	assert.Nil(t, err)
	returnableActionMapping := NewActionMapping(stateModel)

	assert.NotNil(t, returnableActionMapping)

	mapCompare(t, returnableActionMapping.borrowerStateActionMapping, borrowerStateActionMapping)

	mapCompare(t, returnableActionMapping.lenderStateActionMapping, lenderStateActionMapping)
}

var actionMappingService = ActionMappingService{}

func TestIsActionAvailable(t *testing.T) {
	// Borrower
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionValidate))
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}, BorrowerActionReceive))

	// Lender
	assert.True(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, LenderActionShip))
	assert.False(t, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).IsActionAvailable(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}, BorrowerActionRejectCondition))
}

func TestGetActionsForPatronRequest(t *testing.T) {
	// Borrower
	listCompare(t, []pr_db.PatronRequestAction{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateNew}))
	listCompare(t, []pr_db.PatronRequestAction{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideBorrowing, State: BorrowerStateCompleted}))

	// Lender
	listCompare(t, []pr_db.PatronRequestAction{LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateWillSupply}))
	listCompare(t, []pr_db.PatronRequestAction{}, actionMappingService.GetActionMapping(pr_db.PatronRequest{}).GetActionsForPatronRequest(pr_db.PatronRequest{Side: SideLending, State: LenderStateShipped}))
}

func listCompare(t *testing.T, list1 []pr_db.PatronRequestAction, list2 []pr_db.PatronRequestAction) {
	assert.Equal(t, len(list1), len(list2))
	for i := range list1 {
		assert.True(t, slices.Contains(list2, list1[i]))
	}
}

func mapCompare(t *testing.T, map1 map[pr_db.PatronRequestState][]pr_db.PatronRequestAction, map2 map[pr_db.PatronRequestState][]pr_db.PatronRequestAction) {
	for stateName := range map1 {
		listOne := map1[stateName]
		listTwo := map2[stateName]
		assert.Equal(t, len(listOne), len(listTwo))
		for i := range listOne {
			assert.True(t, slices.Contains(listTwo, listOne[i]))
		}
	}
}
