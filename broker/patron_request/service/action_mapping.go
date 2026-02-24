package prservice

import (
	"slices"
	"strings"

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
	borrowerStateConfig        map[pr_db.PatronRequestState]stateConfig
	lenderStateConfig          map[pr_db.PatronRequestState]stateConfig
}

type stateConfig struct {
	actions     map[pr_db.PatronRequestAction]proapi.ModelAction
	events      map[string]proapi.ModelEvent
	autoActions []pr_db.PatronRequestAction
}

// Constructor function to initialize the mappings for given StateModel
func NewActionMapping(stateModel *proapi.StateModel) *ActionMapping {
	r := new(ActionMapping)
	if stateModel == nil || stateModel.States == nil {
		return r
	}

	borrowerMap := make(map[pr_db.PatronRequestState][]pr_db.PatronRequestAction)
	lenderMap := make(map[pr_db.PatronRequestState][]pr_db.PatronRequestAction)
	borrowerConfig := make(map[pr_db.PatronRequestState]stateConfig)
	lenderConfig := make(map[pr_db.PatronRequestState]stateConfig)

	for _, state := range stateModel.States {
		stateName := pr_db.PatronRequestState(state.Name)
		currentStateConfig := stateConfig{
			actions: make(map[pr_db.PatronRequestAction]proapi.ModelAction),
			events:  make(map[string]proapi.ModelEvent),
		}
		manualActions := make([]pr_db.PatronRequestAction, 0)
		if state.Actions != nil {
			for _, action := range *state.Actions {
				actionName := pr_db.PatronRequestAction(action.Name)
				currentStateConfig.actions[actionName] = action
				if action.Trigger != nil && strings.EqualFold(string(*action.Trigger), string(proapi.Auto)) {
					currentStateConfig.autoActions = append(currentStateConfig.autoActions, actionName)
					continue
				}
				manualActions = append(manualActions, actionName)
			}
		}
		if state.Events != nil {
			for _, event := range *state.Events {
				currentStateConfig.events[event.Name] = event
			}
		}

		if state.Side == proapi.REQUESTER {
			borrowerMap[stateName] = manualActions
			borrowerConfig[stateName] = currentStateConfig
		} else if state.Side == proapi.SUPPLIER {
			lenderMap[stateName] = manualActions
			lenderConfig[stateName] = currentStateConfig
		}
	}

	r.borrowerStateActionMapping = borrowerMap
	r.lenderStateActionMapping = lenderMap
	r.borrowerStateConfig = borrowerConfig
	r.lenderStateConfig = lenderConfig

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

func (r *ActionMapping) IsActionSupported(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool {
	stateConfig, ok := r.getStateConfig(pr)
	if !ok {
		return false
	}
	_, ok = stateConfig.actions[action]
	return ok
}

func (r *ActionMapping) GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction {
	if pr.Side == SideBorrowing {
		return getActionsByStateFromMapping(pr.State, r.borrowerStateActionMapping)
	} else {
		return getActionsByStateFromMapping(pr.State, r.lenderStateActionMapping)
	}
}

func (r *ActionMapping) GetActionTransition(pr pr_db.PatronRequest, action pr_db.PatronRequestAction, outcome string) (pr_db.PatronRequestState, bool) {
	stateConfig, ok := r.getStateConfig(pr)
	if !ok {
		return "", false
	}
	actionConfig, ok := stateConfig.actions[action]
	if !ok {
		return "", false
	}
	if actionConfig.Transitions == nil {
		return "", false
	}
	if outcome == ActionOutcomeSuccess && actionConfig.Transitions.Success != nil && *actionConfig.Transitions.Success != "" {
		return pr_db.PatronRequestState(*actionConfig.Transitions.Success), true
	}
	if outcome == ActionOutcomeFailure && actionConfig.Transitions.Failure != nil && *actionConfig.Transitions.Failure != "" {
		return pr_db.PatronRequestState(*actionConfig.Transitions.Failure), true
	}
	return "", false
}

func (r *ActionMapping) GetEventTransition(pr pr_db.PatronRequest, eventName string) (pr_db.PatronRequestState, bool, bool) {
	stateConfig, ok := r.getStateConfig(pr)
	if !ok {
		return "", false, false
	}
	eventConfig, ok := stateConfig.events[eventName]
	if !ok {
		return "", false, false
	}
	if eventConfig.Transition == nil || *eventConfig.Transition == "" {
		return "", false, true
	}
	return pr_db.PatronRequestState(*eventConfig.Transition), true, true
}

func (r *ActionMapping) GetAutoActionsForState(pr pr_db.PatronRequest) []pr_db.PatronRequestAction {
	stateConfig, ok := r.getStateConfig(pr)
	if !ok || len(stateConfig.autoActions) == 0 {
		return []pr_db.PatronRequestAction{}
	}
	return append([]pr_db.PatronRequestAction{}, stateConfig.autoActions...)
}

func (r *ActionMapping) getStateConfig(pr pr_db.PatronRequest) (stateConfig, bool) {
	if pr.Side == SideBorrowing {
		cfg, ok := r.borrowerStateConfig[pr.State]
		return cfg, ok
	}
	cfg, ok := r.lenderStateConfig[pr.State]
	return cfg, ok
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
