package prservice

import (
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"slices"
)

type ActionMapping interface {
	IsActionAvailable(pr pr_db.PatronRequest, action string) bool
	GetActionsForPatronRequest(pr pr_db.PatronRequest) []string
}

var returnableBorrowerStateActionMapping = map[string][]string{
	BorrowerStateNew:              {ActionValidate},
	BorrowerStateValidated:        {ActionSendRequest},
	BorrowerStateSupplierLocated:  {ActionCancelRequest},
	BorrowerStateConditionPending: {ActionAcceptCondition, ActionRejectCondition},
	BorrowerStateWillSupply:       {ActionCancelRequest},
	BorrowerStateShipped:          {ActionReceive},
	BorrowerStateReceived:         {ActionCheckOut},
	BorrowerStateCheckedOut:       {ActionCheckIn},
	BorrowerStateCheckedIn:        {ActionShipReturn},
}
var returnableLenderStateActionMapping = map[string][]string{
	LenderStateNew:               {ActionValidate},
	LenderStateValidated:         {ActionWillSupply, ActionCannotSupply, ActionAddCondition},
	LenderStateWillSupply:        {ActionAddCondition, ActionCannotSupply, ActionShip},
	LenderStateConditionPending:  {ActionCannotSupply},
	LenderStateConditionAccepted: {ActionShip, ActionCannotSupply},
	LenderStateShippedReturn:     {ActionMarkReceived},
	LenderStateCancelRequested:   {ActionMarkCancelled, ActionWillSupply},
}

type ActionMappingService struct {
}

func (r *ActionMappingService) GetActionMapping(pr pr_db.PatronRequest) ActionMapping {
	return new(ReturnableActionMapping)
}

type ReturnableActionMapping struct {
}

func (r *ReturnableActionMapping) IsActionAvailable(pr pr_db.PatronRequest, action string) bool {
	if pr.Side == SideBorrowing {
		return isActionAvailable(pr.State, action, returnableBorrowerStateActionMapping)
	} else {
		return isActionAvailable(pr.State, action, returnableLenderStateActionMapping)
	}
}
func (r *ReturnableActionMapping) GetActionsForPatronRequest(pr pr_db.PatronRequest) []string {
	if pr.Side == SideBorrowing {
		return getActionsByStateFromMapping(pr.State, returnableBorrowerStateActionMapping)
	} else {
		return getActionsByStateFromMapping(pr.State, returnableLenderStateActionMapping)
	}
}

func isActionAvailable(state string, action string, actionMapping map[string][]string) bool {
	return slices.Contains(getActionsByStateFromMapping(state, actionMapping), action)
}
func getActionsByStateFromMapping(state string, actionMapping map[string][]string) []string {
	if actions, ok := actionMapping[state]; ok {
		return actions
	} else {
		return []string{}
	}
}
