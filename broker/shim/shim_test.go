package shim

import (
	"encoding/xml"
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
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
				Note:             "original note",
			},
			ReturnInfo: &iso18626.ReturnInfo{
				Name: "University of Chicago (ISIL:US-IL-UC)",
				PhysicalAddress: &iso18626.PhysicalAddress{
					Line1:    "124 Main St",
					Line2:    "",
					Locality: "Chicago",
					Region: &iso18626.TypeSchemeValuePair{
						Text: "IL",
					},
					PostalCode: "60606",
					Country: &iso18626.TypeSchemeValuePair{
						Text: "US",
					},
				},
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
	assert.Equal(t, "original note\n"+
		SUPPLIER_BEGIN+"\nUniversity of Chicago (ISIL:US-IL-UC)\n"+SUPPLIER_END+"\n\n"+
		RETURN_ADDRESS_BEGIN+"\n124 Main St\nChicago, IL, 60606\nUS\n"+RETURN_ADDRESS_END+"\n",
		resmsg.SupplyingAgencyMessage.MessageInfo.Note)
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

func TestIso18626AlmaShimRequest(t *testing.T) {
	msg := iso18626.ISO18626Message{
		Request: &iso18626.Request{
			RequestingAgencyInfo: &iso18626.RequestingAgencyInfo{
				Name: "University of Chicago (ISIL:US-IL-UC)",
			},
			RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
				{
					Address: &iso18626.Address{
						PhysicalAddress: &iso18626.PhysicalAddress{
							Line1:    "124 Main St",
							Line2:    "",
							Locality: "Chicago",
							Region: &iso18626.TypeSchemeValuePair{
								Text: "IL",
							},
							PostalCode: "60606",
							Country: &iso18626.TypeSchemeValuePair{
								Text: "US",
							},
						},
					},
				},
			},
			ServiceInfo: &iso18626.ServiceInfo{
				Note: "original note",
			},
		},
	}
	shim := GetShim(VendorAlma)
	bytes, err := shim.ApplyToOutgoing(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = shim.ApplyToIncoming(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to apply incoming")
	}
	assert.Equal(t, "original note\n"+
		REQUESTER_BEGIN+"\nUniversity of Chicago (ISIL:US-IL-UC)\n"+REQUESTER_END+"\n\n"+
		DELIVERY_ADDRESS_BEGIN+"\n124 Main St\nChicago, IL, 60606\nUS\n"+DELIVERY_ADDRESS_END+"\n",
		resmsg.Request.ServiceInfo.Note)
}
