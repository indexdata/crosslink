package prservice

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/indexdata/crosslink/broker/patron_request/proapi"
)

type StateModelService struct {
	stateMap map[string]*proapi.StateModel
	mu       sync.RWMutex
}

func (s *StateModelService) GetStateModel(modelName string) (*proapi.StateModel, error) {
	s.mu.RLock()
	if s.stateMap != nil {
		if stateModel, ok := s.stateMap[modelName]; ok {
			s.mu.RUnlock()
			return stateModel, nil
		}
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stateMap == nil {
		s.stateMap = make(map[string]*proapi.StateModel)
	}

	stateModel, ok := s.stateMap[modelName]

	if ok {
		return stateModel, nil
	}

	stateModel, err := LoadStateModelByName(modelName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	s.stateMap[modelName] = stateModel
	return stateModel, nil
}

//go:embed statemodels
var modelFS embed.FS

func LoadStateModelByName(modelName string) (*proapi.StateModel, error) {
	path := "statemodels/" + modelName + ".json"
	stateModel, err := loadStateModel(path)
	if err != nil {
		return nil, err
	}
	if err = ValidateStateModel(stateModel); err != nil {
		return nil, err
	}
	return stateModel, nil
}

func loadStateModel(path string) (*proapi.StateModel, error) {
	data, err := modelFS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stateModel proapi.StateModel
	err = json.Unmarshal(data, &stateModel)
	return &stateModel, err
}

func ValidateStateModel(stateModel *proapi.StateModel) error {
	if stateModel == nil {
		return fmt.Errorf("state model is nil")
	}
	c := BuiltInStateModelCapabilities()
	definedStates := map[proapi.ModelStateSide]map[string]struct{}{
		proapi.REQUESTER: {},
		proapi.SUPPLIER:  {},
	}
	// Pass 1: validate all states and collect the state set defined in this model.
	for _, state := range stateModel.States {
		var builtInStates []string
		switch state.Side {
		case proapi.REQUESTER:
			builtInStates = c.RequesterStates
		case proapi.SUPPLIER:
			builtInStates = c.SupplierStates
		default:
			return fmt.Errorf("state %s has unsupported side %s", state.Name, state.Side)
		}
		if !slices.Contains(builtInStates, state.Name) {
			return fmt.Errorf("state %s is not a built-in %s state", state.Name, strings.ToLower(string(state.Side)))
		}
		sideStates := definedStates[state.Side]
		if _, exists := sideStates[state.Name]; exists {
			return fmt.Errorf("state %s is defined multiple times for side %s", state.Name, state.Side)
		}
		sideStates[state.Name] = struct{}{}
	}

	// Pass 2: validate actions/events and their transitions.
	for _, state := range stateModel.States {
		var allowedActions []proapi.ActionCapability
		var allowedEvents []string
		var allowedEventsSide string
		allowedTransitionTargets := definedStates[state.Side]
		if state.Side == proapi.REQUESTER {
			allowedActions = c.RequesterActions
			allowedEvents = c.SupplierMessageEvents
			allowedEventsSide = strings.ToLower(string(proapi.SUPPLIER))
		} else {
			allowedActions = c.SupplierActions
			allowedEvents = c.RequesterMessageEvents
			allowedEventsSide = strings.ToLower(string(proapi.REQUESTER))
		}
		if state.Actions != nil {
			for _, action := range *state.Actions {
				found := false
				for _, allowedAction := range allowedActions {
					if action.Name == allowedAction.Name {
						found = true
					}
				}
				if !found {
					return fmt.Errorf("action %s in state %s is not a built-in %s action", action.Name, state.Name, strings.ToLower(string(state.Side)))
				}
				if err := validateActionTransitions(action, state.Name, allowedTransitionTargets); err != nil {
					return err
				}
			}
		}
		if state.Events != nil {
			for _, event := range *state.Events {
				if !slices.Contains(allowedEvents, event.Name) {
					return fmt.Errorf("event %s in state %s is not a built-in %s message event", event.Name, state.Name, allowedEventsSide)
				}
				if err := validateEventTransition(event, state.Name, allowedTransitionTargets); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func validateActionTransitions(action proapi.ModelAction, stateName string, allowedTransitionTargets map[string]struct{}) error {
	if action.Transitions == nil {
		return nil
	}
	if action.Transitions.Success != nil && *action.Transitions.Success != "" {
		target := *action.Transitions.Success
		if !hasTransitionTarget(allowedTransitionTargets, target) {
			return fmt.Errorf("action %s in state %s has invalid success transition target %s", action.Name, stateName, target)
		}
	}
	if action.Transitions.Failure != nil && *action.Transitions.Failure != "" {
		target := *action.Transitions.Failure
		if !hasTransitionTarget(allowedTransitionTargets, target) {
			return fmt.Errorf("action %s in state %s has invalid failure transition target %s", action.Name, stateName, target)
		}
	}
	return nil
}

func validateEventTransition(event proapi.ModelEvent, stateName string, allowedTransitionTargets map[string]struct{}) error {
	if event.Transition == nil || *event.Transition == "" {
		return nil
	}
	target := *event.Transition
	if !hasTransitionTarget(allowedTransitionTargets, target) {
		return fmt.Errorf("event %s in state %s has invalid transition target %s", event.Name, stateName, target)
	}
	return nil
}

func hasTransitionTarget(allowedTransitionTargets map[string]struct{}, name string) bool {
	_, ok := allowedTransitionTargets[name]
	return ok
}
