package shim

import (
	"encoding/xml"
	"github.com/indexdata/crosslink/broker/common"
	"os"
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
	shim := GetShim(string(common.VendorAlma))
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
				Note:             "#seq:12#original note",
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
	shim := GetShim(string(common.VendorAlma))
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
		RETURN_ADDRESS_BEGIN+"\nUniversity of Chicago (ISIL:US-IL-UC)\n124 Main St\nChicago, IL, 60606\nUS\n"+RETURN_ADDRESS_END+"\n",
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
	shim := GetShim(string(common.VendorAlma))
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
	shim := GetShim(string(common.VendorAlma))
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
				Note: "#seq:0#original note",
				ServiceLevel: &iso18626.TypeSchemeValuePair{
					Text: "secondarymail",
				},
			},
			SupplierInfo: []iso18626.SupplierInfo{
				{
					SupplierDescription: RETURN_ADDRESS_BEGIN + "\nsome address\n" + RETURN_ADDRESS_END + "\n",
				},
			},
		},
	}
	msg.Request.BibliographicInfo.SupplierUniqueRecordId = "12345678"
	isbn := iso18626.BibliographicItemId{
		BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
			Text: "isbn",
		},
		BibliographicItemIdentifier: "978-3-16-148410-0",
	}
	badItemId := iso18626.BibliographicItemId{
		BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
			Text: "badcode",
		},
		BibliographicItemIdentifier: "val",
	}
	msg.Request.BibliographicInfo.BibliographicItemId = append(msg.Request.BibliographicInfo.BibliographicItemId, isbn, badItemId)
	lccn := iso18626.BibliographicRecordId{
		BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{
			Text: "lccn",
		},
		BibliographicRecordIdentifier: "2023000023",
	}
	badRecId := iso18626.BibliographicRecordId{
		BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{
			Text: "lccnNumber",
		},
		BibliographicRecordIdentifier: "val",
	}
	msg.Request.BibliographicInfo.BibliographicRecordId = append(msg.Request.BibliographicInfo.BibliographicRecordId, lccn, badRecId)
	shim := GetShim(string(common.VendorAlma))
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
		DELIVERY_ADDRESS_BEGIN+"\nUniversity of Chicago (ISIL:US-IL-UC)\n124 Main St\nChicago, IL, 60606\nUS\n"+DELIVERY_ADDRESS_END+"\n"+"\n"+
		RETURN_ADDRESS_BEGIN+"\nsome address\n"+RETURN_ADDRESS_END+"\n",
		resmsg.Request.ServiceInfo.Note)
	assert.Equal(t, "SecondaryMail", resmsg.Request.ServiceInfo.ServiceLevel.Text)
	assert.Equal(t, 1, len(resmsg.Request.BibliographicInfo.BibliographicItemId))
	assert.Equal(t, "ISBN", resmsg.Request.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text)
	assert.Equal(t, "978-3-16-148410-0", resmsg.Request.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier)
	assert.Equal(t, 2, len(resmsg.Request.BibliographicInfo.BibliographicRecordId))
	assert.Equal(t, "LCCN", resmsg.Request.BibliographicInfo.BibliographicRecordId[0].BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "2023000023", resmsg.Request.BibliographicInfo.BibliographicRecordId[0].BibliographicRecordIdentifier)
	//we pass supplierUniqeRecordId as the OCLC number
	assert.Equal(t, "OCLC", resmsg.Request.BibliographicInfo.BibliographicRecordId[1].BibliographicRecordIdentifierCode.Text)
	assert.Equal(t, "12345678", resmsg.Request.BibliographicInfo.BibliographicRecordId[1].BibliographicRecordIdentifier)
}

func TestIso18626ReShareShimSupplyingMessageLoanConditions(t *testing.T) {
	data, _ := os.ReadFile("../test/testdata/supmsg-notification-conditions.xml")

	var resmsg iso18626.ISO18626Message
	err := GetShim(string(common.VendorReShare)).ApplyToIncoming(data, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, "Conditions pending \nPlease respond `ACCEPT` or `REJECT`", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626ReShareShimRequestingMessageLoanConditionAccept(t *testing.T) {
	msg := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Action: iso18626.TypeActionNotification,
			Note:   "Accept",
		},
	}
	msgBytes, err := GetShim(string(common.VendorReShare)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)

	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, "#ReShareLoanConditionAgreeResponse#", resmsg.RequestingAgencyMessage.Note)
}

func TestIso18626ReShareShimRequestingMessageLoanConditionReject(t *testing.T) {
	msg := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Action: iso18626.TypeActionNotification,
			Note:   "ReJeCT",
		},
	}
	msgBytes, err := GetShim(string(common.VendorReShare)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)

	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, "ReJeCT", resmsg.RequestingAgencyMessage.Note)
	assert.Equal(t, iso18626.TypeActionCancel, resmsg.RequestingAgencyMessage.Action)
}
