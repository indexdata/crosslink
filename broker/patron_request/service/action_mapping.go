package prservice

import (
	"slices"
	"strings"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
)

type ActionMappingService struct {
	SMService *StateModelService
}

func (r *ActionMappingService) NewActionMappingService() *ActionMappingService {
	return &ActionMappingService{
		SMService: &StateModelService{},
	}
}

func (r *ActionMappingService) GetActionMapping(pr pr_db.PatronRequest) (*ActionMapping, error) {
	//TODO: check the PatronRequest loan type to decide what kind of state model/mapping to return
	stateModel, err := r.getStateModelService().GetStateModel("returnables")
	if err != nil {
		return nil, err
	}
	return NewActionMapping(stateModel), nil
}

func (r *ActionMappingService) GetStateModel(modelName string) (*proapi.StateModel, error) {
	return r.getStateModelService().GetStateModel(modelName)
}

func (r *ActionMappingService) getStateModelService() *StateModelService {
	if r.SMService == nil {
		r.SMService = &StateModelService{}
	}
	return r.SMService
}

type ActionEntry struct {
	name pr_db.PatronRequestAction
	auto bool
}

type ActionMapping struct {
	borrowerStateActionMapping map[pr_db.PatronRequestState][]ActionEntry
	lenderStateActionMapping   map[pr_db.PatronRequestState][]ActionEntry
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

	borrowerMap := make(map[pr_db.PatronRequestState][]ActionEntry)
	lenderMap := make(map[pr_db.PatronRequestState][]ActionEntry)
	borrowerConfig := make(map[pr_db.PatronRequestState]stateConfig)
	lenderConfig := make(map[pr_db.PatronRequestState]stateConfig)

	for _, state := range stateModel.States {
		stateName := pr_db.PatronRequestState(state.Name)
		currentStateConfig := stateConfig{
			actions: make(map[pr_db.PatronRequestAction]proapi.ModelAction),
			events:  make(map[string]proapi.ModelEvent),
		}
		actionEntries := make([]ActionEntry, 0)
		if state.Actions != nil {
			for _, action := range *state.Actions {
				entry := ActionEntry{name: pr_db.PatronRequestAction(action.Name)}
				currentStateConfig.actions[entry.name] = action
				if action.Trigger != nil && strings.EqualFold(string(*action.Trigger), string(proapi.Auto)) {
					currentStateConfig.autoActions = append(currentStateConfig.autoActions, entry.name)
					entry.auto = true
				}
				actionEntries = append(actionEntries, entry)
			}
		}
		if state.Events != nil {
			for _, event := range *state.Events {
				currentStateConfig.events[event.Name] = event
			}
		}

		switch state.Side {
		case proapi.REQUESTER:
			borrowerMap[stateName] = actionEntries
			borrowerConfig[stateName] = currentStateConfig
		case proapi.SUPPLIER:
			lenderMap[stateName] = actionEntries
			lenderConfig[stateName] = currentStateConfig
		}
	}

	r.borrowerStateActionMapping = borrowerMap
	r.lenderStateActionMapping = lenderMap
	r.borrowerStateConfig = borrowerConfig
	r.lenderStateConfig = lenderConfig

	return r
}

func (r *ActionMapping) GetActionsForPatronRequest(pr pr_db.PatronRequest) []pr_db.PatronRequestAction {
	actions := make([]pr_db.PatronRequestAction, 0)

	prLastActionFailed := strings.EqualFold(pr.LastActionResult.String, string(events.EventStatusError)) ||
		strings.EqualFold(pr.LastActionResult.String, string(events.EventStatusProblem))
	hasFailed := false
	var actionEntries []ActionEntry
	if pr.Side == SideBorrowing {
		actionEntries = r.borrowerStateActionMapping[pr.State]
	} else {
		actionEntries = r.lenderStateActionMapping[pr.State]
	}
	for _, action := range actionEntries {
		if pr.LastAction.String == string(action.name) && prLastActionFailed {
			hasFailed = true
		}
		if !action.auto || hasFailed {
			actionName := pr_db.PatronRequestAction(action.name)
			actions = append(actions, actionName)
		}
	}
	return actions
}

func (r *ActionMapping) IsActionAvailable(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool {
	actions := r.GetActionsForPatronRequest(pr)
	return slices.Contains(actions, action)
}

func (r *ActionMapping) IsActionSupported(pr pr_db.PatronRequest, action pr_db.PatronRequestAction) bool {
	stateConfig, ok := r.getStateConfig(pr)
	if !ok {
		return false
	}
	_, ok = stateConfig.actions[action]
	return ok
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
