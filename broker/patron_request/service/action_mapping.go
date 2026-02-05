package prservice

import (
	"slices"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
)

type ActionMappingService struct {
	SMService StateModelService
}

func (r *ActionMappingService) NewActionMappingService() *ActionMappingService {
	return &ActionMappingService{
		SMService: StateModelService{},
	}
}

func (r *ActionMappingService) GetActionMapping(pr pr_db.PatronRequest) *ActionMapping {
	//TODO: check the PatronRequest loan type to decide what kind of state model/mapping to return
	return NewActionMapping(r.SMService.GetStateModel("returnables"))
}

type ActionMapping struct {
	borrowerStateActionMapping map[pr_db.PatronRequestState][]pr_db.PatronRequestAction
	lenderStateActionMapping   map[pr_db.PatronRequestState][]pr_db.PatronRequestAction
}

// Constructor function to initialize the mappings for given StateModel
func NewActionMapping(stateModel *proapi.StateModel) *ActionMapping {
	r := new(ActionMapping)

	borrowerMap := make(map[pr_db.PatronRequestState][]pr_db.PatronRequestAction)
	lenderMap := make(map[pr_db.PatronRequestState][]pr_db.PatronRequestAction)

	for _, state := range *stateModel.States {
		if state.Actions != nil {
			nameList := make([]pr_db.PatronRequestAction, 0)
			for _, action := range *state.Actions {
				nameList = append(nameList, pr_db.PatronRequestAction(action.Name))
			}
			if state.Side == proapi.REQUESTER {
				borrowerMap[pr_db.PatronRequestState(state.Name)] = nameList
			} else {
				lenderMap[pr_db.PatronRequestState(state.Name)] = nameList
			}
		}
	}

	r.borrowerStateActionMapping = borrowerMap
	r.lenderStateActionMapping = lenderMap

	return r
}

func (r *ActionMapping) GetBorrowerActionsMap() map[pr_db.PatronRequestState][]pr_db.PatronRequestAction {
	return r.borrowerStateActionMapping
}

func (r *ActionMapping) GetLenderActionsMap() map[pr_db.PatronRequestState][]pr_db.PatronRequestAction {
	return r.lenderStateActionMapping
}

func (r *ActionMapping) IsActionAvailable(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool {
	if pr.Side == SideBorrowing {
		return isActionAvailable(pr.State, action, r.borrowerStateActionMapping)
	} else {
		return isActionAvailable(pr.State, action, r.lenderStateActionMapping)
	}
}

func (r *ActionMapping) GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction {
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
