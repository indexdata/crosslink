package shim

import (
	"encoding/xml"
	"testing"

	"github.com/indexdata/crosslink/iso18626"
)

func TestIso18626AlmaShimLoanCompleted(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusLoanCompleted,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
		},
	}
	shim := GetShim(VendorAlma)
	bytes, err := shim.ApplyToOutgoing(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to parse xml")
	}
	if resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage != iso18626.TypeReasonForMessageRequestResponse {
		t.Errorf("expected to have message reason %s but got %s", iso18626.TypeReasonForMessageRequestResponse,
			resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	}
}

func TestIso18626AlmaShimLoanLoaned(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusLoaned,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageRequestResponse,
			},
		},
	}
	shim := GetShim(VendorAlma)
	bytes, err := shim.ApplyToOutgoing(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to parse xml")
	}
	if resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage != iso18626.TypeReasonForMessageStatusChange {
		t.Errorf("expected to have message reason %s but got %s", iso18626.TypeReasonForMessageStatusChange,
			resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	}
	//loaned message should not be changed if reasonForMessage is not request response
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRenewResponse
	bytes, err = shim.ApplyToOutgoing(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	err = xml.Unmarshal(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to parse xml")
	}
	if resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageStatusChange {
		t.Errorf("expected to have message reason %s but got %s", iso18626.TypeReasonForMessageRenewResponse,
			resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	}
}

func TestIso18626AlmaShimIncoming(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusLoaned,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageRequestResponse,
			},
		},
	}
	bytes, err := xml.Marshal(&msg)
	if err != nil {
		t.Errorf("failed to marshal xml")
	}
	var resmsg iso18626.ISO18626Message
	shim := GetShim(VendorAlma)
	err = shim.ApplyToIncoming(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to apply incoming")
	}
}

func TestIso18626AlmaShimWillSupply(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusWillSupply,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
		},
	}
	shim := GetShim(VendorAlma)
	bytes, err := shim.ApplyToOutgoing(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to parse xml")
	}
	if resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage != iso18626.TypeReasonForMessageRequestResponse {
		t.Errorf("expected to have message reason %s but got %s", iso18626.TypeReasonForMessageRequestResponse,
			resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	}
}

func TestIso18626DefaultShim(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusLoaned,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
		},
	}
	shim := GetShim("other")
	bytes, err := shim.ApplyToOutgoing(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = shim.ApplyToIncoming(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to apply incoming")
	}
}
