package prservice

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/indexdata/crosslink/broker/patron_request/proapi"
)

var errNotFound = fmt.Errorf("state model not found")

const (
	defaultStateModelName = "default"
	legacyStateModelName  = "returnables"
)

func canonicalStateModelName(modelName string) string {
	if modelName == legacyStateModelName {
		return defaultStateModelName
	}
	return modelName
}

type StateModelService struct {
	stateMap         map[string]*proapi.StateModel
	actionMappingMap map[actionMappingKey]*ActionMapping
	mu               sync.RWMutex
	actionMappingMu  sync.RWMutex
}

type actionMappingKey struct {
	modelName   string
	serviceType proapi.StateModelServiceType
}

type StateModelsConfig struct {
	StateModels         map[string]proapi.StateModel `json:"stateModels"`
	BatchActionDefaults []proapi.BatchActionDefault  `json:"batchActionDefaults"`
	TemplateDefaults    []proapi.CreateTemplate      `json:"templateDefaults"`
}

func (s *StateModelService) GetStateModel(modelName string) (*proapi.StateModel, error) {
	modelName = canonicalStateModelName(modelName)
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
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	s.stateMap[modelName] = stateModel
	return stateModel, nil
}

func (s *StateModelService) GetActionMapping(modelName string, serviceType proapi.StateModelServiceType) (*ActionMapping, error) {
	modelName = canonicalStateModelName(modelName)
	key := actionMappingKey{modelName: modelName, serviceType: serviceType}
	s.actionMappingMu.RLock()
	if mapping, ok := s.actionMappingMap[key]; ok {
		s.actionMappingMu.RUnlock()
		return mapping, nil
	}
	s.actionMappingMu.RUnlock()

	stateModel, err := s.GetStateModel(modelName)
	if err != nil {
		return nil, err
	}
	if stateModel == nil {
		return nil, errNotFound
	}
	mapping := NewActionMappingForServiceType(stateModel, serviceType)

	s.actionMappingMu.Lock()
	defer s.actionMappingMu.Unlock()
	if existing, ok := s.actionMappingMap[key]; ok {
		return existing, nil
	}
	if s.actionMappingMap == nil {
		s.actionMappingMap = make(map[actionMappingKey]*ActionMapping)
	}
	s.actionMappingMap[key] = mapping
	return mapping, nil
}

//go:embed statemodels/state-models.json
var stateModelsFile []byte
var stateModelsConfig StateModelsConfig

// LoadStateModelByName returns a deep copy of the named state model so that
// callers (and the validation/caching layer) cannot accidentally mutate the
// global embedded defaults through shared slice/map backing arrays.
func LoadStateModelByName(modelName string) (*proapi.StateModel, error) {
	modelName = canonicalStateModelName(modelName)
	src, ok := stateModelsConfig.StateModels[modelName]
	if !ok {
		return nil, errNotFound
	}
	// Deep-copy via JSON round-trip: marshal the source value then unmarshal
	// into a fresh struct, ensuring no slice or map is shared with the global
	// stateModelsConfig.
	raw, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state model %q: %w", modelName, err)
	}
	var stateModel proapi.StateModel
	if err := json.Unmarshal(raw, &stateModel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state model %q: %w", modelName, err)
	}
	if err := ValidateStateModel(&stateModel); err != nil {
		return nil, err
	}
	return &stateModel, nil
}

func init() {
	if err := json.Unmarshal(stateModelsFile, &stateModelsConfig); err != nil {
		panic("failed to parse state-models.json: " + err.Error())
	}
}

func ValidateStateModel(stateModel *proapi.StateModel) error {
	if stateModel == nil {
		return fmt.Errorf("state model is nil")
	}
	serviceTypes, contextualErrors, err := validateApplicability(stateModel)
	if err != nil {
		return err
	}
	for _, serviceType := range serviceTypes {
		if err := validateStateModelForServiceType(stateModel, serviceType); err != nil {
			if !contextualErrors {
				return err
			}
			return fmt.Errorf("state model is invalid for service type %s: %w", serviceType, err)
		}
	}
	return nil
}

func validateApplicability(stateModel *proapi.StateModel) ([]proapi.StateModelServiceType, bool, error) {
	var serviceTypes []proapi.StateModelServiceType
	selected := make(map[proapi.StateModelServiceType]struct{})
	if stateModel.Selector != nil {
		if len(stateModel.Selector.ServiceType) == 0 {
			return nil, false, fmt.Errorf("state model selector serviceType must not be empty")
		}
		for _, serviceType := range stateModel.Selector.ServiceType {
			if !serviceType.Valid() {
				return nil, false, fmt.Errorf("state model selector contains unsupported service type %s", serviceType)
			}
			if _, duplicate := selected[serviceType]; duplicate {
				return nil, false, fmt.Errorf("state model selector contains duplicate service type %s", serviceType)
			}
			selected[serviceType] = struct{}{}
			serviceTypes = append(serviceTypes, serviceType)
		}
	}

	hasAppliesTo := false
	validate := func(label string, field string, appliesTo *proapi.AppliesTo) error {
		if appliesTo == nil {
			return nil
		}
		hasAppliesTo = true
		if len(appliesTo.ServiceTypes) == 0 {
			return fmt.Errorf("%s %s.serviceTypes must not be empty", label, field)
		}
		seen := make(map[proapi.StateModelServiceType]struct{})
		for _, serviceType := range appliesTo.ServiceTypes {
			if !serviceType.Valid() {
				return fmt.Errorf("%s %s contains unsupported service type %s", label, field, serviceType)
			}
			if _, duplicate := seen[serviceType]; duplicate {
				return fmt.Errorf("%s %s contains duplicate service type %s", label, field, serviceType)
			}
			seen[serviceType] = struct{}{}
			if stateModel.Selector != nil {
				if _, ok := selected[serviceType]; !ok {
					return fmt.Errorf("%s %s includes service type %s not handled by the state model selector", label, field, serviceType)
				}
			}
		}
		return nil
	}

	for _, state := range stateModel.States {
		stateLabel := fmt.Sprintf("state %s side %s", state.Name, state.Side)
		if err := validate(stateLabel, "appliesTo", state.AppliesTo); err != nil {
			return nil, false, err
		}
		if state.Actions != nil {
			for _, action := range *state.Actions {
				actionLabel := fmt.Sprintf("action %s in %s", action.Name, stateLabel)
				if err := validate(actionLabel, "appliesTo", action.AppliesTo); err != nil {
					return nil, false, err
				}
				if err := validate(actionLabel, "primaryFor", action.PrimaryFor); err != nil {
					return nil, false, err
				}
				if err := validateApplicabilitySubset(actionLabel, action.AppliesTo, state.AppliesTo); err != nil {
					return nil, false, err
				}
				if err := validateApplicabilitySubset(actionLabel+" primaryFor", action.PrimaryFor, action.AppliesTo); err != nil {
					return nil, false, err
				}
				if err := validateApplicabilitySubset(actionLabel+" primaryFor", action.PrimaryFor, state.AppliesTo); err != nil {
					return nil, false, err
				}
			}
		}
		if state.Events != nil {
			for _, event := range *state.Events {
				eventLabel := fmt.Sprintf("event %s in %s", event.Name, stateLabel)
				if err := validate(eventLabel, "appliesTo", event.AppliesTo); err != nil {
					return nil, false, err
				}
				if err := validateApplicabilitySubset(eventLabel, event.AppliesTo, state.AppliesTo); err != nil {
					return nil, false, err
				}
			}
		}
	}
	if stateModel.Selector == nil {
		if hasAppliesTo {
			serviceTypes = []proapi.StateModelServiceType{proapi.Copy, proapi.Loan, proapi.CopyOrLoan}
		} else {
			// The choice is immaterial for an unconditional model and preserves the
			// historical, unqualified validation error messages.
			serviceTypes = []proapi.StateModelServiceType{proapi.Loan}
		}
	}
	return serviceTypes, stateModel.Selector != nil || hasAppliesTo, nil
}

func validateApplicabilitySubset(label string, child *proapi.AppliesTo, parent *proapi.AppliesTo) error {
	if child == nil || parent == nil {
		return nil
	}
	for _, serviceType := range child.ServiceTypes {
		if !slices.Contains(parent.ServiceTypes, serviceType) {
			return fmt.Errorf("%s includes service type %s excluded by its parent", label, serviceType)
		}
	}
	return nil
}

func validateStateModelForServiceType(stateModel *proapi.StateModel, serviceType proapi.StateModelServiceType) error {
	c := BuiltInStateModelCapabilities()
	definedStates := map[proapi.ModelStateSide]map[string]struct{}{
		proapi.REQUESTER: {},
		proapi.SUPPLIER:  {},
	}
	terminalStates := map[proapi.ModelStateSide]map[string]struct{}{
		proapi.REQUESTER: {},
		proapi.SUPPLIER:  {},
	}
	manualCloseStates := map[proapi.ModelStateSide]string{}
	initialStates := map[proapi.ModelStateSide]string{}
	// Pass 1: validate all states and collect the state set defined in this model.

	for _, state := range stateModel.States {
		if !appliesToServiceType(state.AppliesTo, serviceType) {
			continue
		}
		var builtInStates []string
		switch state.Side {
		case proapi.REQUESTER:
			builtInStates = c.RequesterStates
		case proapi.SUPPLIER:
			builtInStates = c.SupplierStates
		default:
			return fmt.Errorf("state %s has unsupported side %s", state.Name, state.Side)
		}
		if initial, ok := initialStates[state.Side]; ok && state.Initial != nil && *state.Initial {
			return fmt.Errorf("initial state defined multiple times for side %s: %s and %s", state.Side, initial, state.Name)
		}
		if state.Initial != nil && *state.Initial {
			initialStates[state.Side] = state.Name
		}
		if !slices.Contains(builtInStates, state.Name) {
			return fmt.Errorf("state %s is not a built-in %s state", state.Name, strings.ToLower(string(state.Side)))
		}
		sideStates := definedStates[state.Side]
		if _, exists := sideStates[state.Name]; exists {
			return fmt.Errorf("state %s is defined multiple times for side %s", state.Name, state.Side)
		}
		sideStates[state.Name] = struct{}{}
		if state.Terminal != nil && *state.Terminal {
			terminalStates[state.Side][state.Name] = struct{}{}
		}
		if state.ManualClose != nil && *state.ManualClose {
			if state.Terminal == nil || !*state.Terminal {
				return fmt.Errorf("manualClose state %s side %s must be terminal", state.Name, state.Side)
			}
			if existing, exists := manualCloseStates[state.Side]; exists {
				return fmt.Errorf("manualClose state defined multiple times for side %s: %s and %s", state.Side, existing, state.Name)
			}
			manualCloseStates[state.Side] = state.Name
		}
	}

	// Pass 2: check that each side has an initial state defined if it has any states defined.
	for side, sideStates := range definedStates {
		if len(sideStates) > 0 {
			if initial, ok := initialStates[side]; !ok || initial == "" {
				return fmt.Errorf("initial state not defined for side %s", side)
			}
		}
	}

	// Pass 3: validate actions/events and their transitions.
	for _, state := range stateModel.States {
		if !appliesToServiceType(state.AppliesTo, serviceType) {
			continue
		}
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
				if !appliesToServiceType(action.AppliesTo, serviceType) {
					continue
				}
				capabilityIndex := slices.IndexFunc(allowedActions, func(a proapi.ActionCapability) bool {
					return a.Name == action.Name
				})
				if capabilityIndex == -1 {
					return fmt.Errorf("action %s in state %s is not a built-in %s action", action.Name, state.Name, strings.ToLower(string(state.Side)))
				}
				if err := validateActionTransitions(action, state.Name, allowedTransitionTargets, isTransitionCapability(allowedActions[capabilityIndex])); err != nil {
					return err
				}
			}
		}
		primaryActions := make(map[string]struct{})
		if state.Actions != nil {
			for _, action := range *state.Actions {
				if !appliesToServiceType(action.AppliesTo, serviceType) {
					continue
				}
				if action.PrimaryFor != nil && appliesToServiceType(action.PrimaryFor, serviceType) {
					primaryActions[action.Name] = struct{}{}
				}
				if state.PrimaryAction != nil && action.Name == *state.PrimaryAction {
					primaryActions[action.Name] = struct{}{}
				}
			}
		}
		if state.PrimaryAction != nil {
			if _, found := primaryActions[*state.PrimaryAction]; !found {
				return fmt.Errorf("primary action %s undefined in state %s side %s", *state.PrimaryAction, state.Name, state.Side)
			}
		}
		if len(primaryActions) > 1 {
			return fmt.Errorf("primary action defined multiple times in state %s side %s", state.Name, state.Side)
		}

		if state.ClosingAction != nil {
			closingActionIndex := -1
			if state.Actions != nil {
				closingActionIndex = slices.IndexFunc(*state.Actions, func(a proapi.ModelAction) bool {
					return appliesToServiceType(a.AppliesTo, serviceType) && a.Name == string(*state.ClosingAction)
				})
			}
			if closingActionIndex == -1 {
				return fmt.Errorf("closing action %s undefined in state %s side %s", *state.ClosingAction, state.Name, state.Side)
			}
			closingAction := (*state.Actions)[closingActionIndex]
			if closingAction.Transitions == nil || closingAction.Transitions.Success == nil || *closingAction.Transitions.Success == "" {
				return fmt.Errorf("closing action %s in state %s side %s must define a success transition", *state.ClosingAction, state.Name, state.Side)
			}
			target := *closingAction.Transitions.Success
			if _, terminal := terminalStates[state.Side][target]; !terminal {
				return fmt.Errorf("closing action %s in state %s side %s has non-terminal success transition target %s", *state.ClosingAction, state.Name, state.Side, target)
			}
		}
		if state.Events != nil {
			for _, event := range *state.Events {
				if !appliesToServiceType(event.AppliesTo, serviceType) {
					continue
				}
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

func validateActionTransitions(action proapi.ModelAction, stateName string, allowedTransitionTargets map[string]struct{}, transitionAction bool) error {
	if transitionAction &&
		(action.Transitions == nil || action.Transitions.Success == nil || *action.Transitions.Success == "") {
		return fmt.Errorf("transition action %s in state %s must define a success transition", action.Name, stateName)
	}
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
	if action.Transitions.Review != nil && *action.Transitions.Review != "" {
		target := *action.Transitions.Review
		if !hasTransitionTarget(allowedTransitionTargets, target) {
			return fmt.Errorf("action %s in state %s has invalid review transition target %s", action.Name, stateName, target)
		}
	}
	if action.Transitions.Duplicate != nil && *action.Transitions.Duplicate != "" {
		target := *action.Transitions.Duplicate
		if !hasTransitionTarget(allowedTransitionTargets, target) {
			return fmt.Errorf("action %s in state %s has invalid duplicate transition target %s", action.Name, stateName, target)
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

func GetStateModelBatchActionDefaults() []proapi.BatchActionDefault {
	// Return a defensive deep copy so callers can't mutate embedded defaults.
	data, err := json.Marshal(stateModelsConfig.BatchActionDefaults)
	if err != nil {
		return slices.Clone(stateModelsConfig.BatchActionDefaults)
	}
	var out []proapi.BatchActionDefault
	if err := json.Unmarshal(data, &out); err != nil {
		return slices.Clone(stateModelsConfig.BatchActionDefaults)
	}
	return out
}

func GetStateModelTemplateDefaults() []proapi.CreateTemplate {
	// Return a deep copy so callers can't mutate embedded defaults.
	data, err := json.Marshal(stateModelsConfig.TemplateDefaults)
	if err != nil {
		return slices.Clone(stateModelsConfig.TemplateDefaults)
	}
	var out []proapi.CreateTemplate
	if err := json.Unmarshal(data, &out); err != nil {
		return slices.Clone(stateModelsConfig.TemplateDefaults)
	}
	return out
}
