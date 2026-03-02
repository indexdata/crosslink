package prservice

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
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
	allStates := append([]string{}, c.RequesterStates...)
	allStates = append(allStates, c.SupplierStates...)

	for _, state := range stateModel.States {
		switch state.Side {
		case proapi.REQUESTER:
			if !slices.Contains(c.RequesterStates, state.Name) {
				return fmt.Errorf("state %s is not a built-in requester state", state.Name)
			}
			if state.Actions != nil {
				for _, action := range *state.Actions {
					if !slices.Contains(c.RequesterActions, action.Name) {
						return fmt.Errorf("action %s in state %s is not a built-in requester action", action.Name, state.Name)
					}
					if err := validateActionTransitions(action, state.Name, allStates); err != nil {
						return err
					}
				}
			}
		case proapi.SUPPLIER:
			if !slices.Contains(c.SupplierStates, state.Name) {
				return fmt.Errorf("state %s is not a built-in supplier state", state.Name)
			}
			if state.Actions != nil {
				for _, action := range *state.Actions {
					if !slices.Contains(c.SupplierActions, action.Name) {
						return fmt.Errorf("action %s in state %s is not a built-in supplier action", action.Name, state.Name)
					}
					if err := validateActionTransitions(action, state.Name, allStates); err != nil {
						return err
					}
				}
			}
		default:
			return fmt.Errorf("state %s has unsupported side %s", state.Name, state.Side)
		}

		if state.Events != nil {
			for _, event := range *state.Events {
				if !slices.Contains(c.MessageEvents, event.Name) {
					return fmt.Errorf("event %s in state %s is not a built-in message event", event.Name, state.Name)
				}
				if event.Transition != nil && *event.Transition != "" && !slices.Contains(allStates, *event.Transition) {
					return fmt.Errorf("event %s in state %s has invalid transition target %s", event.Name, state.Name, *event.Transition)
				}
			}
		}
	}

	return nil
}

func validateActionTransitions(action proapi.ModelAction, stateName string, allStates []string) error {
	if action.Transitions == nil {
		return nil
	}
	if action.Transitions.Success != nil && *action.Transitions.Success != "" && !slices.Contains(allStates, *action.Transitions.Success) {
		return fmt.Errorf("action %s in state %s has invalid success transition target %s", action.Name, stateName, *action.Transitions.Success)
	}
	if action.Transitions.Failure != nil && *action.Transitions.Failure != "" && !slices.Contains(allStates, *action.Transitions.Failure) {
		return fmt.Errorf("action %s in state %s has invalid failure transition target %s", action.Name, stateName, *action.Transitions.Failure)
	}
	return nil
}
