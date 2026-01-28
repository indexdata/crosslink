package prservice

import (
	"slices"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
)

type ActionMapping interface {
	IsActionAvailable(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool
	GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction
	GetBorrowerActionsMap() map[pr_db.PatronRequestState][]pr_db.PatronRequestAction
	GetLenderActionsMap() map[pr_db.PatronRequestState][]pr_db.PatronRequestAction
}

type ActionMappingService struct {
}

func (r *ActionMappingService) GetActionMapping(pr pr_db.PatronRequest) ActionMapping {
	return new(ReturnableActionMapping)
}

type ReturnableActionMapping struct {
	borrowerStateActionMapping map[pr_db.PatronRequestState][]pr_db.PatronRequestAction
	lenderStateActionMapping   map[pr_db.PatronRequestState][]pr_db.PatronRequestAction
}

/* Constructor function to initialize the mappings for the returnables */
func NewReturnableActionMapping() *ReturnableActionMapping {
	r := new(ReturnableActionMapping)
	r.borrowerStateActionMapping = map[pr_db.PatronRequestState][]pr_db.PatronRequestAction{
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
	r.lenderStateActionMapping = map[pr_db.PatronRequestState][]pr_db.PatronRequestAction{
		LenderStateNew:               {LenderActionValidate},
		LenderStateValidated:         {LenderActionWillSupply, LenderActionCannotSupply, LenderActionAddCondition},
		LenderStateWillSupply:        {LenderActionAddCondition, LenderActionCannotSupply, LenderActionShip},
		LenderStateConditionPending:  {LenderActionCannotSupply},
		LenderStateConditionAccepted: {LenderActionShip, LenderActionCannotSupply},
		LenderStateShippedReturn:     {LenderActionMarkReceived},
		LenderStateCancelRequested:   {LenderActionMarkCancelled, LenderActionWillSupply},
	}
	return r
}

func (r *ReturnableActionMapping) GetBorrowerActionsMap() map[pr_db.PatronRequestState][]pr_db.PatronRequestAction {
	return r.borrowerStateActionMapping
}

func (r *ReturnableActionMapping) GetLenderActionsMap() map[pr_db.PatronRequestState][]pr_db.PatronRequestAction {
	return r.lenderStateActionMapping
}

func (r *ReturnableActionMapping) IsActionAvailable(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool {
	if pr.Side == SideBorrowing {
		return isActionAvailable(pr.State, action, r.borrowerStateActionMapping)
	} else {
		return isActionAvailable(pr.State, action, r.lenderStateActionMapping)
	}
}
func (r *ReturnableActionMapping) GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction {
	if pr.Side == SideBorrowing {
		return getActionsByStateFromMapping(pr.State, r.borrowerStateActionMapping)
	} else {
		return getActionsByStateFromMapping(pr.State, r.lenderStateActionMapping)
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
