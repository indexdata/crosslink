package prservice

import (
	"slices"
	"testing"

	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/stretchr/testify/assert"
)

func TestBuiltInStateModelCapabilities(t *testing.T) {
	c := BuiltInStateModelCapabilities()
	assert.True(t, slices.Contains(c.RequesterStates, string(BorrowerStateValidated)))
	assert.True(t, slices.Contains(c.SupplierStates, string(LenderStateValidated)))
	assert.True(t, slices.Contains(c.RequesterActions, string(BorrowerActionSendRequest)))
	assert.True(t, slices.Contains(c.SupplierActions, string(LenderActionWillSupply)))
	assert.True(t, slices.Contains(c.MessageEvents, string(SupplierWillSupply)))
}

func TestValidateStateModelInvalidRequesterAction(t *testing.T) {
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name: "NEW",
				Side: proapi.REQUESTER,
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
	model := &proapi.StateModel{
		Type:    proapi.StateModelTypeStateModel,
		Name:    "test",
		Version: "1.0.0",
		States: []proapi.ModelState{
			{
				Name: "SENT",
				Side: proapi.REQUESTER,
				Events: &[]proapi.ModelEvent{
					{Name: "not-an-event"},
				},
			},
		},
	}

	err := ValidateStateModel(model)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a built-in message event")
}
