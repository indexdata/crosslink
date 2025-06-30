package shim

import (
	"encoding/xml"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/go-utils/utils"

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
				Note:             "thanks for sending it back",
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
		},
	}
	shim := GetShim(string(common.VendorAlma))
	bytes, err := shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, "thanks for sending it back", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimCopyCompleted(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusCopyCompleted,
			},
			MessageInfo: iso18626.MessageInfo{
				Note:             "sending you the URL",
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
			DeliveryInfo: &iso18626.DeliveryInfo{
				SentVia: &iso18626.TypeSchemeValuePair{
					Text: "URL",
				},
				ItemId: "http://example.com/item/12345678",
			},
		},
	}
	shim := GetShim(string(common.VendorAlma))
	bytes, err := shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusCopyCompleted, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, URL_PRE+"http://example.com/item/12345678"+NOTE_FIELD_SEP+
		"sending you the URL", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
	//apply again that URL is added once
	bytes, err = shim.ApplyToOutgoing(&resmsg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg2 iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg2)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, URL_PRE+"http://example.com/item/12345678"+NOTE_FIELD_SEP+
		"sending you the URL", resmsg2.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimCopyCompletedEmail(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusCopyCompleted,
			},
			MessageInfo: iso18626.MessageInfo{
				Note:             "sending you the email",
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
			DeliveryInfo: &iso18626.DeliveryInfo{
				SentVia: &iso18626.TypeSchemeValuePair{
					Text: "Email",
				},
			},
		},
	}
	shim := GetShim(string(common.VendorAlma))
	bytes, err := shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusCopyCompleted, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, "sending you the email", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
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
			DeliveryInfo: &iso18626.DeliveryInfo{
				LoanCondition: &iso18626.TypeSchemeValuePair{
					Text: "libraryuseonly",
				},
			},
		},
	}
	shim := GetShim(string(common.VendorAlma))
	bytes, err := shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange,
		resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	//loaned message should not be changed if reasonForMessage is not request response
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRenewResponse
	bytes, err = shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.NotEqual(t, iso18626.TypeReasonForMessageStatusChange, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, LOAN_CONDITION_PRE+string(iso18626.LoanConditionLibraryUseOnly)+NOTE_FIELD_SEP+
		"original note\n"+
		RETURN_ADDRESS_BEGIN+"\nUniversity of Chicago (ISIL:US-IL-UC)\n124 Main St\nChicago, IL, 60606\nUS\n"+RETURN_ADDRESS_END+"\n",
		resmsg.SupplyingAgencyMessage.MessageInfo.Note)
	assert.Equal(t, string(iso18626.LoanConditionLibraryUseOnly), resmsg.SupplyingAgencyMessage.DeliveryInfo.LoanCondition.Text)
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

func TestIso18626AlmaShimExpectToSupply(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusExpectToSupply,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
			},
		},
	}
	shim := GetShim(string(common.VendorAlma))
	bytes, err := shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusWillSupply, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
}

func TestIso18626AlmaShimNotificationRequestReceived(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusRequestReceived,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			},
		},
	}
	shim := GetShim(string(common.VendorAlma))
	bytes, err := shim.ApplyToOutgoing(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusWillSupply, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
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
			PublicationInfo: &iso18626.PublicationInfo{
				PublicationType: &iso18626.TypeSchemeValuePair{
					Text: "book",
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
	//fix publication type
	assert.Equal(t, "Book", resmsg.Request.PublicationInfo.PublicationType.Text)
}

func TestIso18626AlmaShimStripReqSeqMsg(t *testing.T) {
	msg := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Action: iso18626.TypeActionNotification,
			Note:   "#seq:2#original note",
		},
	}
	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)

	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, "original note", resmsg.RequestingAgencyMessage.Note)
}

func TestIso18626AlmaShimSupplyingMessageLoanConditions(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			MessageInfo: iso18626.MessageInfo{
				Note: RESHARE_SUPPLIER_AWAITING_CONDITION + "#seq:1#",
			},
		},
	}

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, ALMA_SUPPLIER_AWAITING_CONDITION, resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimSupplyingMessageAddLoanCondition(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			MessageInfo: iso18626.MessageInfo{
				Note: RESHARE_ADD_LOAN_CONDITION + "#seq:1#",
			},
			DeliveryInfo: &iso18626.DeliveryInfo{
				LoanCondition: &iso18626.TypeSchemeValuePair{
					Text: "libraryuseonly",
				},
			},
		},
	}

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)
	assert.Equal(t, LOAN_CONDITION_PRE+string(iso18626.LoanConditionLibraryUseOnly), resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimSupplyingMessageAddLoanConditionWithNote(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			MessageInfo: iso18626.MessageInfo{
				Note: RESHARE_ADD_LOAN_CONDITION + "staff note#seq:1#",
				OfferedCosts: &iso18626.TypeCosts{
					MonetaryValue: utils.XSDDecimal{
						Base: 20,
					},
					CurrencyCode: iso18626.TypeSchemeValuePair{
						Text: "EUR",
					},
				},
			},
			DeliveryInfo: &iso18626.DeliveryInfo{
				LoanCondition: &iso18626.TypeSchemeValuePair{
					Text: "libraryuseonly",
				},
			},
		},
	}

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, LOAN_CONDITION_PRE+"LibraryUseOnly"+NOTE_FIELD_SEP+
		COST_CONDITION_PRE+"20 EUR"+NOTE_FIELD_SEP+
		"staff note", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimSupplyingMessageLoanConditionsAssumedAgreed(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			MessageInfo: iso18626.MessageInfo{
				Note: RESHARE_SUPPLIER_CONDITIONS_ASSUMED_AGREED + "#seq:1#",
			},
		},
	}

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoing(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncoming(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, ALMA_SUPPLIER_CONDITIONS_ASSUMED_AGREED, resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimRequestingMessageLoanConditionAccept(t *testing.T) {
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

	assert.Equal(t, RESHARE_LOAN_CONDITION_AGREE+"Accept", resmsg.RequestingAgencyMessage.Note)
}

func TestIso18626AlmaShimRequestingMessageLoanConditionReject(t *testing.T) {
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
