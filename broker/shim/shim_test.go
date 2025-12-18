package shim

import (
	"encoding/xml"
	"github.com/indexdata/crosslink/broker/ill_db"
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusCopyCompleted, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, URL_PRE+"http://example.com/item/12345678"+NOTE_FIELD_SEP+
		"sending you the URL", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
	//apply again that URL is added once
	bytes, err = shim.ApplyToOutgoingRequest(&resmsg)
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange,
		resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	//loaned message should not be changed if reasonForMessage is not request response
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRenewResponse
	bytes, err = shim.ApplyToOutgoingRequest(&msg)
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
			DeliveryInfo: &iso18626.DeliveryInfo{
				DeliveryCosts: &iso18626.TypeCosts{
					MonetaryValue: utils.XSDDecimal{
						Base: 1010,
						Exp:  2,
					},
					CurrencyCode: iso18626.TypeSchemeValuePair{
						Text: "USD",
					},
				},
			},
		},
	}
	bytes, err := xml.Marshal(&msg)
	if err != nil {
		t.Errorf("failed to marshal xml")
	}
	assert.Equal(t, iso18626.TypeStatusLoaned, msg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.NotNil(t, msg.SupplyingAgencyMessage.DeliveryInfo, "DeliveryInfo should not be nil")
	assert.NotNil(t, msg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should not be nil")
	assert.Equal(t, 1010, msg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Base, "DeliveryCosts.Base should be 1010")
	assert.Equal(t, 2, msg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Exp, "DeliveryCosts.Exp should be 2")
	assert.Nil(t, msg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should be nil")

	var resmsg iso18626.ISO18626Message
	shim := GetShim(string(common.VendorAlma))
	err = shim.ApplyToIncomingResponse(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to apply incoming")
	}
	assert.Equal(t, iso18626.TypeStatusLoaned, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo, "DeliveryInfo should not be nil")
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should not be nil")
	assert.Equal(t, 1010, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Base, "DeliveryCosts.Base should be 1010")
	assert.Equal(t, 2, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Exp, "DeliveryCosts.Exp should be 2")
	assert.Nil(t, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should not be nil")
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusWillSupply, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	//set loan condition
	msg.SupplyingAgencyMessage.DeliveryInfo = &iso18626.DeliveryInfo{
		LoanCondition: &iso18626.TypeSchemeValuePair{
			Text: "libraryuseonly",
		},
	}
	bytes, err = shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusWillSupply, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, LOAN_CONDITION_PRE+string(iso18626.LoanConditionLibraryUseOnly), resmsg.SupplyingAgencyMessage.MessageInfo.Note)
	assert.Nil(t, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should be nil")
	assert.Nil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should be nil")
	//set cost
	msg.SupplyingAgencyMessage.DeliveryInfo.LoanCondition = nil
	msg.SupplyingAgencyMessage.MessageInfo.Note = ""
	msg.SupplyingAgencyMessage.MessageInfo.OfferedCosts = &iso18626.TypeCosts{
		MonetaryValue: utils.XSDDecimal{
			Base: 20,
		},
		CurrencyCode: iso18626.TypeSchemeValuePair{
			Text: "EUR",
		},
	}
	bytes, err = shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusWillSupply, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, COST_CONDITION_PRE+"20 EUR", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should not be nil")
	assert.Equal(t, 20, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Base, "OfferedCosts.Base should be 20")
	assert.Equal(t, 0, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Exp, "OfferedCosts.Exp should be 0")
	assert.Equal(t, "EUR", resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.CurrencyCode.Text, "OfferedCosts.CurrencyCode should be EUR")
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo, "DeliveryInfo should not be nil")
	assert.Nil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should be nil")
	//change to loaned and verify that offered costs are moved to delivery costs
	msg.SupplyingAgencyMessage.StatusInfo.Status = iso18626.TypeStatusLoaned
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
	bytes, err = shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusLoaned, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, COST_CONDITION_PRE+"20 EUR", resmsg.SupplyingAgencyMessage.MessageInfo.Note)
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should not be nil")
	assert.Equal(t, 20, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Base, "OfferedCosts.Base should be 20")
	assert.Equal(t, 0, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Exp, "OfferedCosts.Exp should be 0")
	assert.Equal(t, "EUR", resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.CurrencyCode.Text, "OfferedCosts.CurrencyCode should be EUR")
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo, "DeliveryInfo should not be nil")
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should not be nil")
	assert.Equal(t, 20, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Base, "DeliveryCosts.Base should be 20")
	assert.Equal(t, 0, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Exp, "DeliveryCosts.Exp should be 0")
	assert.Equal(t, "EUR", resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.CurrencyCode.Text, "DeliveryCosts.CurrencyCode should be EUR")
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err, "failed to apply outgoing")
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	assert.Nil(t, err, "failed to parse xml")
	assert.Equal(t, iso18626.TypeStatusRequestReceived, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = shim.ApplyToIncomingResponse(bytes, &resmsg)
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
			BillingInfo: &iso18626.BillingInfo{
				MaximumCosts: &iso18626.TypeCosts{
					MonetaryValue: utils.XSDDecimal{
						Base: 1061,
						Exp:  2,
					},
					CurrencyCode: iso18626.TypeSchemeValuePair{
						Text: "USD",
					},
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
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = shim.ApplyToIncomingResponse(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to apply incoming")
	}
	assert.Equal(t, "original note\n"+
		COST_CONDITION_PRE+"10.61 USD"+"\n"+
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
	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err)

	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncomingResponse(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, "original note", resmsg.RequestingAgencyMessage.Note)
}

func TestIso18626AlmaShimHumanizeReShareRequesterNote(t *testing.T) {
	msg := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Action: iso18626.TypeActionNotification,
			Note:   RESHARE_LOAN_CONDITION_AGREE + "Accept",
		},
	}
	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err)

	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncomingResponse(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, "Accept", resmsg.RequestingAgencyMessage.Note)
}

func TestIso18626AlmaShimSupplyingMessageLoanConditions(t *testing.T) {
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			MessageInfo: iso18626.MessageInfo{
				Note: RESHARE_SUPPLIER_AWAITING_CONDITION + "#seq:1#",
			},
		},
	}

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncomingResponse(msgBytes, &resmsg)
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

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncomingResponse(msgBytes, &resmsg)
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

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncomingResponse(msgBytes, &resmsg)
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

	msgBytes, err := GetShim(string(common.VendorAlma)).ApplyToOutgoingRequest(&msg)
	assert.Nil(t, err)
	var resmsg iso18626.ISO18626Message
	err = GetShim("default").ApplyToIncomingResponse(msgBytes, &resmsg)
	assert.Nil(t, err)

	assert.Equal(t, ALMA_SUPPLIER_CONDITIONS_ASSUMED_AGREED, resmsg.SupplyingAgencyMessage.MessageInfo.Note)
}

func TestIso18626AlmaShimRequestingMessageLoanConditionAccept(t *testing.T) {
	msg := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: "ISL",
					},
					AgencyIdValue: "BROKER",
				},
			},
			Action: iso18626.TypeActionNotification,
			Note:   "`Accept`",
		},
	}
	resmsg := GetShim(string(common.VendorAlma)).ApplyToIncomingRequest(&msg, nil, &ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP1"})

	assert.Equal(t, RESHARE_LOAN_CONDITION_AGREE+"`Accept`", resmsg.RequestingAgencyMessage.Note)
	assert.Equal(t, "BROKER", resmsg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)

	msg.RequestingAgencyMessage.Note = "I will accept"
	resmsg = GetShim(string(common.VendorAlma)).ApplyToIncomingRequest(&msg, nil, &ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP1"})

	assert.Equal(t, "I will accept", resmsg.RequestingAgencyMessage.Note)
	assert.Equal(t, "BROKER", resmsg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}

func TestIso18626AlmaShimRequestingMessageLoanConditionReject(t *testing.T) {
	msg := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: "ISL",
					},
					AgencyIdValue: "BROKER",
				},
			},
			Action: iso18626.TypeActionNotification,
			Note:   "--ReJeCT;",
		},
	}
	resmsg := GetShim(string(common.VendorAlma)).ApplyToIncomingRequest(&msg, nil, &ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP1"})

	assert.Equal(t, "--ReJeCT;", resmsg.RequestingAgencyMessage.Note)
	assert.Equal(t, iso18626.TypeActionCancel, resmsg.RequestingAgencyMessage.Action)
	assert.Equal(t, "SUP1", resmsg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)

	assert.Equal(t, iso18626.TypeActionNotification, msg.RequestingAgencyMessage.Action)
	assert.Equal(t, "BROKER", msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}

func TestIso18626AReShareShimSupplyingOutgoing(t *testing.T) {
	OFFERED_COSTS = true // ensure that offered costs are enabled
	msg := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			StatusInfo: iso18626.StatusInfo{
				Status: iso18626.TypeStatusLoaned,
			},
			MessageInfo: iso18626.MessageInfo{
				ReasonForMessage: iso18626.TypeReasonForMessageRequestResponse,
			},
			DeliveryInfo: &iso18626.DeliveryInfo{
				DeliveryCosts: &iso18626.TypeCosts{
					MonetaryValue: utils.XSDDecimal{
						Base: 1010,
						Exp:  2,
					},
					CurrencyCode: iso18626.TypeSchemeValuePair{
						Text: "USD",
					},
				},
			},
		},
	}
	assert.Equal(t, iso18626.TypeStatusLoaned, msg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.NotNil(t, msg.SupplyingAgencyMessage.DeliveryInfo, "DeliveryInfo should not be nil")
	assert.NotNil(t, msg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should not be nil")
	assert.Equal(t, 1010, msg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Base, "DeliveryCosts.Base should be 1010")
	assert.Equal(t, 2, msg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Exp, "DeliveryCosts.Exp should be 2")
	assert.Nil(t, msg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should be nil")
	assert.Nil(t, msg.SupplyingAgencyMessage.DeliveryInfo.LoanCondition, "LoanCondition should be nil")

	shim := GetShim(string(common.VendorReShare))
	bytes, err := shim.ApplyToOutgoingRequest(&msg)
	if err != nil {
		t.Errorf("failed to apply outgoing")
	}
	var resmsg iso18626.ISO18626Message
	err = xml.Unmarshal(bytes, &resmsg)
	if err != nil {
		t.Errorf("failed to unmarshal outgoing")
	}
	assert.Equal(t, iso18626.TypeStatusLoaned, resmsg.SupplyingAgencyMessage.StatusInfo.Status)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, resmsg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo, "DeliveryInfo should not be nil")
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts, "DeliveryCosts should not be nil")
	assert.Equal(t, 1010, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Base, "DeliveryCosts.Base should be 1010")
	assert.Equal(t, 2, resmsg.SupplyingAgencyMessage.DeliveryInfo.DeliveryCosts.MonetaryValue.Exp, "DeliveryCosts.Exp should be 2")
	assert.NotNil(t, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts, "OfferedCosts should not be nil")
	assert.Equal(t, 1010, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Base, "OfferedCosts.Base should be 1010")
	assert.Equal(t, 2, resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Exp, "OfferedCosts.Exp should be 2")
	assert.Equal(t, "USD", resmsg.SupplyingAgencyMessage.MessageInfo.OfferedCosts.CurrencyCode.Text, "OfferedCosts.CurrencyCode should be USD")
	assert.Equal(t, "other", resmsg.SupplyingAgencyMessage.DeliveryInfo.LoanCondition.Text, "LoanCondition should be 'other'")
}

func TestAppendUnfilledStatusAndReasonUnfilled(t *testing.T) {
	shima := new(Iso18626AlmaShim)
	sam := iso18626.SupplyingAgencyMessage{
		StatusInfo: iso18626.StatusInfo{
			Status: iso18626.TypeStatusUnfilled,
		},
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			ReasonUnfilled: &iso18626.TypeSchemeValuePair{
				Text: "Cannot find item",
			},
			Note: "Sorry cannot send",
		},
	}
	shima.appendUnfilledStatusAndReasonUnfilled(&sam)
	assert.Equal(t, "Sorry cannot send, Status: Unfilled, Reason: Cannot find item", sam.MessageInfo.Note)

	// No note
	sam = iso18626.SupplyingAgencyMessage{
		StatusInfo: iso18626.StatusInfo{
			Status: iso18626.TypeStatusUnfilled,
		},
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			ReasonUnfilled: &iso18626.TypeSchemeValuePair{
				Text: "Cannot find item",
			},
		},
	}
	shima.appendUnfilledStatusAndReasonUnfilled(&sam)
	assert.Equal(t, "Status: Unfilled, Reason: Cannot find item", sam.MessageInfo.Note)

	// No reason
	sam = iso18626.SupplyingAgencyMessage{
		StatusInfo: iso18626.StatusInfo{
			Status: iso18626.TypeStatusUnfilled,
		},
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
		},
	}
	shima.appendUnfilledStatusAndReasonUnfilled(&sam)
	assert.Equal(t, "Status: Unfilled", sam.MessageInfo.Note)

	// Not unfilled
	sam = iso18626.SupplyingAgencyMessage{
		StatusInfo: iso18626.StatusInfo{
			Status: iso18626.TypeStatusExpectToSupply,
		},
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
		},
	}
	shima.appendUnfilledStatusAndReasonUnfilled(&sam)
	assert.Equal(t, "", sam.MessageInfo.Note)

	// Not notification
	sam = iso18626.SupplyingAgencyMessage{
		StatusInfo: iso18626.StatusInfo{
			Status: iso18626.TypeStatusUnfilled,
		},
		MessageInfo: iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageStatusChange,
		},
	}
	shima.appendUnfilledStatusAndReasonUnfilled(&sam)
	assert.Equal(t, "", sam.MessageInfo.Note)
}
