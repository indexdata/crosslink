package prservice

import (
	"slices"
	"sync"
	"testing"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestBuiltInStateModelCapabilities(t *testing.T) {
	c := BuiltInStateModelCapabilities()
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateValidated)))
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateInvalidPatron)))
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateLocalSupply)))
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateDuplicate)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateValidated)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateItemPending)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateWillSupplyPending)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateReceived)))

	assert.True(t, slices.ContainsFunc(c.RequesterActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(BorrowerActionValidatePatron) && !isTransitionCapability(a)
	}))
	assert.True(t, slices.ContainsFunc(c.RequesterActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(BorrowerActionReceive)
	}))
	assert.True(t, slices.ContainsFunc(c.RequesterActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(BorrowerActionSupplyDocument) && slices.Equal(a.Parameters, []string{"note", "deliveryUrl"})
	}))
	for _, transitionAction := range []pr_db.PatronRequestAction{
		BorrowerActionSkipPatronValidation,
		BorrowerActionSkipMetadataUpdate,
		BorrowerActionCloseRequest,
		BorrowerActionRejectRetry,
	} {
		assert.True(t, slices.ContainsFunc(c.RequesterActions, func(a proapi.ActionCapability) bool {
			return a.Name == string(transitionAction) && isTransitionCapability(a)
		}))
	}

	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionRequestItem) && len(a.Parameters) == 0
	}))
	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionWillSupply)
	}))
	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionWillSupply) && slices.Contains(a.Parameters, "note")
	}))
	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionSupplyDocument) && slices.Equal(a.Parameters, []string{"note", "deliveryUrl"})
	}))
	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionAddItem) && slices.Equal(a.Parameters, []string{"barcode", "callNumber", "title", "itemId"})
	}))
	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionRemoveItem) && slices.Equal(a.Parameters, []string{"barcode"})
	}))

	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierWillSupply)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierCancelledLocal)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierCompletedLocal)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierUnfilledLocal)))
	assert.True(t, slices.Contains(c.RequesterMessageEvents, string(RequesterCancelRequest)))
	assert.True(t, slices.Contains(c.RequesterMessageEvents, string(RequesterReceived)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierCancelRejected)))
}

func TestUnifiedStateModelDeclaresConditionalCopyWorkflow(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) || !assert.NotNil(t, model) {
		return
	}

	assert.Equal(t, "CrossLink State Model", model.Name)
	if assert.NotNil(t, model.Selector) {
		assert.ElementsMatch(t, []proapi.StateModelServiceType{
			proapi.StateModelServiceType(iso18626.TypeServiceTypeLoan),
			proapi.StateModelServiceType(iso18626.TypeServiceTypeCopy),
			proapi.StateModelServiceType(iso18626.TypeServiceTypeCopyOrLoan),
		}, model.Selector.ServiceType)
	}
	assert.True(t, slices.ContainsFunc(model.States, func(state proapi.ModelState) bool {
		return state.Name == string(LenderStateShipped) && state.AppliesTo != nil &&
			slices.Contains(state.AppliesTo.ServiceTypes, proapi.Loan)
	}))
	assert.True(t, slices.ContainsFunc(model.States, func(state proapi.ModelState) bool {
		if state.Side != proapi.SUPPLIER || state.Name != string(LenderStateWillSupply) || state.Actions == nil {
			return false
		}
		return slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
			return action.Name == string(LenderActionSupplyDocument) && action.PrimaryFor != nil &&
				slices.Equal(action.PrimaryFor.ServiceTypes, []proapi.StateModelServiceType{proapi.Copy}) &&
				action.AppliesTo != nil && slices.Equal(action.AppliesTo.ServiceTypes, []proapi.StateModelServiceType{proapi.Copy, proapi.CopyOrLoan})
		})
	}))
	assert.True(t, slices.ContainsFunc(model.States, func(state proapi.ModelState) bool {
		if state.Side != proapi.REQUESTER || state.Name != string(BorrowerStateLocalSupply) || state.Actions == nil {
			return false
		}
		return slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
			return action.Name == string(BorrowerActionSupplyDocument) && action.PrimaryFor != nil &&
				slices.Equal(action.PrimaryFor.ServiceTypes, []proapi.StateModelServiceType{proapi.Copy}) &&
				action.AppliesTo != nil && slices.Equal(action.AppliesTo.ServiceTypes, []proapi.StateModelServiceType{proapi.Copy, proapi.CopyOrLoan})
		})
	}))
}

func TestLegacyReturnablesStateModelAlias(t *testing.T) {
	service := &StateModelService{}
	defaultModel, err := service.GetStateModel("default")
	assert.NoError(t, err)
	legacyModel, err := service.GetStateModel("returnables")
	assert.NoError(t, err)
	assert.Same(t, defaultModel, legacyModel)
}

func TestValidateStateModelRejectsEmptyAppliesToServiceTypes(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) {
		return
	}

	for stateIndex := range model.States {
		state := &model.States[stateIndex]
		if state.Side == proapi.REQUESTER && state.Name == string(BorrowerStateShipped) {
			state.AppliesTo = &proapi.AppliesTo{}
			break
		}
	}

	err = ValidateStateModel(model)
	assert.EqualError(t, err, "state SHIPPED side REQUESTER appliesTo.serviceTypes must not be empty")
}

func TestValidateStateModelRejectsTransitionToInapplicableState(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) {
		return
	}

	for stateIndex := range model.States {
		state := &model.States[stateIndex]
		if state.Side != proapi.REQUESTER || state.Name != string(BorrowerStateSent) || state.Events == nil {
			continue
		}
		for eventIndex := range *state.Events {
			event := &(*state.Events)[eventIndex]
			if event.Name == string(SupplierCompleted) {
				target := string(BorrowerStateShipped)
				event.Transition = &target
			}
		}
	}

	err = ValidateStateModel(model)
	assert.ErrorContains(t, err, "state model is invalid for service type Copy")
	assert.ErrorContains(t, err, "event completed in state SENT has invalid transition target SHIPPED")
}

func TestValidateSelectorlessStateModelPerServiceType(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) {
		return
	}
	model.Selector = nil

	for stateIndex := range model.States {
		state := &model.States[stateIndex]
		if state.Side != proapi.REQUESTER || state.Name != string(BorrowerStateSent) || state.Events == nil {
			continue
		}
		for eventIndex := range *state.Events {
			event := &(*state.Events)[eventIndex]
			if event.Name == string(SupplierCompleted) {
				target := string(BorrowerStateShipped)
				event.Transition = &target
			}
		}
	}

	err = ValidateStateModel(model)
	assert.ErrorContains(t, err, "state model is invalid for service type Copy")
	assert.ErrorContains(t, err, "event completed in state SENT has invalid transition target SHIPPED")
}

func TestValidateStateModelRejectsChildApplicabilityOutsideState(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) {
		return
	}

	for stateIndex := range model.States {
		state := &model.States[stateIndex]
		if state.Side == proapi.REQUESTER && state.Name == string(BorrowerStateShipped) && state.Actions != nil {
			(*state.Actions)[0].AppliesTo = &proapi.AppliesTo{ServiceTypes: []proapi.StateModelServiceType{proapi.Copy}}
			break
		}
	}

	err = ValidateStateModel(model)
	assert.EqualError(t, err, "action receive in state SHIPPED side REQUESTER includes service type Copy excluded by its parent")
}

func TestDefaultIncludesLocalSupplyRequesterState(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) || !assert.NotNil(t, model) {
		return
	}

	stateIndex := slices.IndexFunc(model.States, func(state proapi.ModelState) bool {
		return state.Name == string(BorrowerStateLocalSupply) && state.Side == proapi.REQUESTER
	})
	assert.NotEqual(t, -1, stateIndex)
}

func TestDefaultInvalidPatronStateIsEditableAndNeedsAttention(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) || !assert.NotNil(t, model) {
		return
	}

	stateIndex := slices.IndexFunc(model.States, func(state proapi.ModelState) bool {
		return state.Name == string(BorrowerStateInvalidPatron) && state.Side == proapi.REQUESTER
	})
	if !assert.NotEqual(t, -1, stateIndex) {
		return
	}
	state := model.States[stateIndex]
	assert.NotNil(t, state.Editable)
	assert.True(t, *state.Editable)
	assert.NotNil(t, state.NeedsAttention)
	assert.True(t, *state.NeedsAttention)
	assert.NotNil(t, state.ClosingAction)
	assert.Equal(t, string(BorrowerActionCloseRequest), *state.ClosingAction)
	assert.True(t, slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
		return action.Name == string(BorrowerActionSkipPatronValidation)
	}))
	assert.True(t, slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
		return action.Name == string(BorrowerActionCloseRequest)
	}))
}

func TestDefaultNewRequesterStateIsEditable(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) || !assert.NotNil(t, model) {
		return
	}

	stateIndex := slices.IndexFunc(model.States, func(state proapi.ModelState) bool {
		return state.Name == string(BorrowerStateNew) && state.Side == proapi.REQUESTER
	})
	if !assert.NotEqual(t, -1, stateIndex) {
		return
	}
	state := model.States[stateIndex]
	assert.NotNil(t, state.Editable)
	assert.True(t, *state.Editable)
	assert.NotNil(t, state.ClosingAction)
	assert.Equal(t, string(BorrowerActionCloseRequest), *state.ClosingAction)
	assert.True(t, slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
		return action.Name == string(BorrowerActionSkipPatronValidation)
	}))
	assert.True(t, slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
		return action.Name == string(BorrowerActionCloseRequest)
	}))
}

func TestDefaultDuplicateStateIsEditableAndNeedsAttention(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) || !assert.NotNil(t, model) {
		return
	}

	stateIndex := slices.IndexFunc(model.States, func(state proapi.ModelState) bool {
		return state.Name == string(BorrowerStateDuplicate) && state.Side == proapi.REQUESTER
	})
	if !assert.NotEqual(t, -1, stateIndex) {
		return
	}
	state := model.States[stateIndex]
	assert.NotNil(t, state.Editable)
	assert.True(t, *state.Editable)
	assert.NotNil(t, state.NeedsAttention)
	assert.True(t, *state.NeedsAttention)
	assert.Nil(t, state.Terminal)
	assert.NotNil(t, state.PrimaryAction)
	assert.Equal(t, string(BorrowerActionSendRequest), *state.PrimaryAction)
	assert.NotNil(t, state.ClosingAction)
	assert.Equal(t, string(BorrowerActionCloseRequest), *state.ClosingAction)
	assert.True(t, slices.ContainsFunc(*state.Actions, func(action proapi.ModelAction) bool {
		return action.Name == string(BorrowerActionSendRequest)
	}))
}

func TestValidateStateModelMissingInitial(t *testing.T) {
	s := "validate-patron"
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name: "NEW",
				Side: proapi.REQUESTER,
				Actions: &[]proapi.ModelAction{
					{Name: s},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Equal(t, "initial state not defined for side REQUESTER", err.Error())
}

func TestValidateStateModelDoubleInitial(t *testing.T) {
	s := "validate-patron"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name: "NEW",
				Side: proapi.REQUESTER,
				Actions: &[]proapi.ModelAction{
					{Name: s},
				},
				Initial: &tt,
			},
			{
				Name: "OTHER",
				Side: proapi.REQUESTER,
				Actions: &[]proapi.ModelAction{
					{Name: s},
				},
				Initial: &tt,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Equal(t, "initial state defined multiple times for side REQUESTER: NEW and OTHER", err.Error())
}

func TestValidateStateModelWithPrimaryAction(t *testing.T) {
	s := "validate-patron"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "NEW",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{Name: s},
				},
				PrimaryAction: &s,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.NoError(t, err)
}

func TestValidateStateModelWithoutPrimaryAction(t *testing.T) {
	s := "validate-patron"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "NEW",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{Name: s},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.NoError(t, err)
}

func TestValidateStateModelPrimaryActionUndefined(t *testing.T) {
	s := "other"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "NEW",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{Name: "validate-patron"},
				},
				PrimaryAction: &s,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Equal(t, "primary action other undefined in state NEW side REQUESTER", err.Error())
}

func TestValidateStateModelPrimaryActionNoActionsDefined(t *testing.T) {
	s := "other"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:          "NEW",
				Side:          proapi.REQUESTER,
				Initial:       &tt,
				Actions:       nil,
				PrimaryAction: &s,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Equal(t, "primary action other undefined in state NEW side REQUESTER", err.Error())
}

func TestValidateStateModelClosingActionUndefined(t *testing.T) {
	s := "ship"
	valid := "will-supply"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "VALIDATED",
				Side:    proapi.SUPPLIER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{Name: valid},
				},
				PrimaryAction: &valid,
				ClosingAction: &s,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Equal(t, "closing action ship undefined in state VALIDATED side SUPPLIER", err.Error())
}

func TestValidateStateModelClosingActionNoActionsDefined(t *testing.T) {
	s := "ship"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:          "VALIDATED",
				Side:          proapi.SUPPLIER,
				Initial:       &tt,
				ClosingAction: &s,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Equal(t, "closing action ship undefined in state VALIDATED side SUPPLIER", err.Error())
}

func TestValidateStateModelClosingActionWithoutSuccessTransition(t *testing.T) {
	closingAction := string(BorrowerActionValidatePatron)
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:          string(BorrowerStateNew),
				Side:          proapi.REQUESTER,
				Initial:       &tt,
				ClosingAction: &closingAction,
				Actions: &[]proapi.ModelAction{
					{Name: closingAction},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.EqualError(t, err, "closing action validate-patron in state NEW side REQUESTER must define a success transition")
}

func TestValidateStateModelClosingActionTransitionsToNonTerminalState(t *testing.T) {
	closingAction := string(BorrowerActionCloseRequest)
	target := string(BorrowerStateValidated)
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:          string(BorrowerStateNew),
				Side:          proapi.REQUESTER,
				Initial:       &tt,
				ClosingAction: &closingAction,
				Actions: &[]proapi.ModelAction{
					{
						Name: closingAction,
						Transitions: &struct {
							Duplicate *string `json:"duplicate,omitempty"`
							Failure   *string `json:"failure,omitempty"`
							Review    *string `json:"review,omitempty"`
							Success   *string `json:"success,omitempty"`
						}{
							Success: &target,
						},
					},
				},
			},
			{
				Name: string(BorrowerStateValidated),
				Side: proapi.REQUESTER,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.EqualError(t, err, "closing action close-request in state NEW side REQUESTER has non-terminal success transition target VALIDATED")
}

func TestValidateStateModelManualCloseTerminal(t *testing.T) {
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:        string(BorrowerStateManuallyClosed),
				Side:        proapi.REQUESTER,
				Initial:     &tt,
				Terminal:    &tt,
				ManualClose: &tt,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.NoError(t, err)
}

func TestValidateStateModelManualCloseNonTerminal(t *testing.T) {
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:        string(BorrowerStateNew),
				Side:        proapi.REQUESTER,
				Initial:     &tt,
				ManualClose: &tt,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be terminal")
}

func TestValidateStateModelManualCloseDuplicateSide(t *testing.T) {
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:        string(BorrowerStateCancelled),
				Side:        proapi.REQUESTER,
				Terminal:    &tt,
				ManualClose: &tt,
			},
			{
				Name:        string(BorrowerStateManuallyClosed),
				Side:        proapi.REQUESTER,
				Terminal:    &tt,
				ManualClose: &tt,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manualClose state defined multiple times")
}

func TestValidateStateModelInvalidRequesterAction(t *testing.T) {
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "NEW",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{Name: "not-an-action"},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a built-in requester action")
}

func TestValidateStateModelInvalidMessageEvent(t *testing.T) {
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "SENT",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Events: &[]proapi.ModelEvent{
					{Name: "not-an-event"},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a built-in supplier message event")
}

func TestValidateStateModelUnsupportedSide(t *testing.T) {
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name: "NEW",
				Side: proapi.ModelStateSide("UNKNOWN"),
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported side")
}

func TestValidateStateModelInvalidActionSuccessTransitionTarget(t *testing.T) {
	invalidTarget := "DOES_NOT_EXIST"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "NEW",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{
						Name: string(BorrowerActionValidatePatron),
						Transitions: &struct {
							Duplicate *string `json:"duplicate,omitempty"`
							Failure   *string `json:"failure,omitempty"`
							Review    *string `json:"review,omitempty"`
							Success   *string `json:"success,omitempty"`
						}{
							Success: &invalidTarget,
						},
					},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid success transition target")
}

func TestValidateStateModelInvalidActionFailureTransitionTarget(t *testing.T) {
	invalidTarget := "DOES_NOT_EXIST"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "VALIDATED",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{
						Name: string(BorrowerActionSendRequest),
						Transitions: &struct {
							Duplicate *string `json:"duplicate,omitempty"`
							Failure   *string `json:"failure,omitempty"`
							Review    *string `json:"review,omitempty"`
							Success   *string `json:"success,omitempty"`
						}{
							Failure: &invalidTarget,
						},
					},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid failure transition target")
}

func TestValidateStateModelInvalidEventTransitionTarget(t *testing.T) {
	invalidTarget := "DOES_NOT_EXIST"
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    "SENT",
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Events: &[]proapi.ModelEvent{
					{
						Name:       string(SupplierWillSupply),
						Transition: &invalidTarget,
					},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition target")
}

func TestValidateStateModelActionTransitionTargetMustExistInModelForSameSide(t *testing.T) {
	transition := string(BorrowerStateValidated)
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    string(BorrowerStateNew),
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{
						Name: string(BorrowerActionValidatePatron),
						Transitions: &struct {
							Duplicate *string `json:"duplicate,omitempty"`
							Failure   *string `json:"failure,omitempty"`
							Review    *string `json:"review,omitempty"`
							Success   *string `json:"success,omitempty"`
						}{
							Success: &transition,
						},
					},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid success transition target")
}

func TestValidateStateModelActionTransitionCannotCrossSides(t *testing.T) {
	transition := string(BorrowerStateValidated)
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    string(BorrowerStateNew),
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Actions: &[]proapi.ModelAction{
					{
						Name: string(BorrowerActionValidatePatron),
						Transitions: &struct {
							Duplicate *string `json:"duplicate,omitempty"`
							Failure   *string `json:"failure,omitempty"`
							Review    *string `json:"review,omitempty"`
							Success   *string `json:"success,omitempty"`
						}{
							Success: &transition,
						},
					},
				},
			},
			{
				Name:    string(LenderStateValidated),
				Side:    proapi.SUPPLIER,
				Initial: &tt,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid success transition target")
}

func TestValidateStateModelEventTransitionCannotCrossSides(t *testing.T) {
	transition := string(BorrowerStateShipped)
	tt := true
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name:    string(BorrowerStateSent),
				Side:    proapi.REQUESTER,
				Initial: &tt,
				Events: &[]proapi.ModelEvent{
					{
						Name:       string(SupplierLoaned),
						Transition: &transition,
					},
				},
			},
			{
				Name:    string(LenderStateShipped),
				Side:    proapi.SUPPLIER,
				Initial: &tt,
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition target")
}

func TestValidateStateModelTransitionActionRequiresSuccessTransition(t *testing.T) {
	model, err := LoadStateModelByName("default")
	if !assert.NoError(t, err) {
		return
	}

	for stateIndex := range model.States {
		state := &model.States[stateIndex]
		if state.Name != string(BorrowerStateInvalidPatron) || state.Actions == nil {
			continue
		}
		for actionIndex := range *state.Actions {
			action := &(*state.Actions)[actionIndex]
			if action.Name == string(BorrowerActionSkipPatronValidation) {
				action.Transitions = nil
			}
		}
	}

	err = ValidateStateModel(model)
	assert.ErrorContains(t, err, "transition action skip-patron-validation in state INVALID_PATRON must define a success transition")
}

func TestStateModelServiceConcurrentGetStateModel(t *testing.T) {
	service := &StateModelService{}
	const goroutines = 50

	var wg sync.WaitGroup
	results := make(chan *proapi.StateModel, goroutines)
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			model, err := service.GetStateModel("default")
			if err != nil {
				errs <- err
				return
			}
			results <- model
		}()
	}

	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}

	var first *proapi.StateModel
	for model := range results {
		assert.NotNil(t, model)
		if first == nil {
			first = model
			continue
		}
		assert.Same(t, first, model)
	}
	assert.NotNil(t, first)
}
