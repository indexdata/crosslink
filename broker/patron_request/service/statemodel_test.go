package prservice

import (
	"slices"
	"sync"
	"testing"

	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/stretchr/testify/assert"
)

func TestBuiltInStateModelCapabilities(t *testing.T) {
	c := BuiltInStateModelCapabilities()
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateValidated)))
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateLocalSupply)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateValidated)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateReceived)))

	assert.True(t, slices.ContainsFunc(c.RequesterActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(BorrowerActionValidate)
	}))
	assert.True(t, slices.ContainsFunc(c.RequesterActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(BorrowerActionReceive)
	}))

	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionWillSupply)
	}))
	assert.True(t, slices.ContainsFunc(c.SupplierActions, func(a proapi.ActionCapability) bool {
		return a.Name == string(LenderActionWillSupply) && slices.Contains(a.Parameters, "note")
	}))

	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierWillSupply)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierCancelledLocal)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierCompletedLocal)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierUnfilledLocal)))
	assert.True(t, slices.Contains(c.RequesterMessageEvents, string(RequesterCancelRequest)))
	assert.True(t, slices.Contains(c.RequesterMessageEvents, string(RequesterReceived)))
	assert.True(t, slices.Contains(c.SupplierMessageEvents, string(SupplierCancelRejected)))
}

func TestReturnablesIncludesLocalSupplyRequesterState(t *testing.T) {
	model, err := LoadStateModelByName("returnables")
	if !assert.NoError(t, err) || !assert.NotNil(t, model) {
		return
	}

	stateIndex := slices.IndexFunc(model.States, func(state proapi.ModelState) bool {
		return state.Name == string(BorrowerStateLocalSupply) && state.Side == proapi.REQUESTER
	})
	assert.NotEqual(t, -1, stateIndex)
}

func TestValidateStateModelMissingInitial(t *testing.T) {
	s := "validate"
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
	s := "validate"
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
	s := "validate"
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
	s := "validate"
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
					{Name: "validate"},
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
						Name: string(BorrowerActionValidate),
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
						Name: string(BorrowerActionValidate),
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
						Name: string(BorrowerActionValidate),
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
			model, err := service.GetStateModel("returnables")
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
