package prservice

import (
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"slices"
)

type ActionMapping interface {
	IsActionAvailable(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool
	GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction
}

var returnableBorrowerStateActionMapping = map[pr_db.PatronRequestState][]pr_db.PatronRequestAction{
	BorrowerStateNew:              {BorrowerActionValidate},
	BorrowerStateValidated:        {BorrowerActionSendRequest},
	BorrowerStateSupplierLocated:  {BorrowerActionCancelRequest},
	BorrowerStateConditionPending: {BorrowerActionAcceptCondition, BorrowerActionRejectCondition},
	BorrowerStateWillSupply:       {BorrowerActionCancelRequest},
	BorrowerStateShipped:          {BorrowerActionReceive},
	BorrowerStateReceived:         {BorrowerActionCheckOut},
	BorrowerStateCheckedOut:       {BorrowerActionCheckIn},
	BorrowerStateCheckedIn:        {BorrowerActionShipReturn},
}
var returnableLenderStateActionMapping = map[pr_db.PatronRequestState][]pr_db.PatronRequestAction{
	LenderStateNew:               {LenderActionValidate},
	LenderStateValidated:         {LenderActionWillSupply, LenderActionCannotSupply, LenderActionAddCondition},
	LenderStateWillSupply:        {LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip},
	LenderStateConditionPending:  {LenderActionCannotSupply},
	LenderStateConditionAccepted: {LenderActionShip, LenderActionCannotSupply},
	LenderStateShippedReturn:     {LenderActionMarkReceived},
	LenderStateCancelRequested:   {LenderActionMarkCancelled, LenderActionWillSupply},
}

type ActionMappingService struct {
}

func (r *ActionMappingService) GetActionMapping(pr pr_db.PatronRequest) ActionMapping {
	return new(ReturnableActionMapping)
}

type ReturnableActionMapping struct {
}

func (r *ReturnableActionMapping) IsActionAvailable(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool {
	if pr.Side == SideBorrowing {
		return isActionAvailable(pr.State, action, returnableBorrowerStateActionMapping)
	} else {
		return isActionAvailable(pr.State, action, returnableLenderStateActionMapping)
	}
}
func (r *ReturnableActionMapping) GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction {
	if pr.Side == SideBorrowing {
		return getActionsByStateFromMapping(pr.State, returnableBorrowerStateActionMapping)
	} else {
		return getActionsByStateFromMapping(pr.State, returnableLenderStateActionMapping)
	}
}

func isActionAvailable(state pr_db.PatronRequestState, action pr_db.PatronRequestAction, actionMapping map[pr_db.PatronRequestState][]pr_db.PatronRequestAction) bool {
	return slices.Contains(getActionsByStateFromMapping(state, actionMapping), action)
}
func getActionsByStateFromMapping(state pr_db.PatronRequestState, actionMapping map[pr_db.PatronRequestState][]pr_db.PatronRequestAction) []pr_db.PatronRequestAction {
	if actions, ok := actionMapping[state]; ok {
		return actions
	} else {
		return []pr_db.PatronRequestAction{}
	}
}
