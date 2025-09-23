package client

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/test/mocks"

	"github.com/indexdata/crosslink/broker/common"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/shim"
	"github.com/indexdata/crosslink/broker/vcs"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestCreateMessageHeaderTransparent(t *testing.T) {
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := createMessageHeader(illTrans, &sup, true, string(common.BrokerModeTransparent))
	assert.Equal(t, "REQ", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := createMessageHeader(illTrans, &sup, false, string(common.BrokerModeTransparent))
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestCreateMessageHeaderOpaque(t *testing.T) {
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := createMessageHeader(illTrans, &sup, true, string(common.BrokerModeOpaque))
	assert.Equal(t, "BROKER", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := createMessageHeader(illTrans, &sup, false, string(common.BrokerModeOpaque))
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "BROKER", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestCreateMessageHeaderTranslucent(t *testing.T) {
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := createMessageHeader(illTrans, &sup, true, string(common.BrokerModeTranslucent))
	assert.Equal(t, "BROKER", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := createMessageHeader(illTrans, &sup, false, string(common.BrokerModeTranslucent))
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "BROKER", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestSendHttpPost(t *testing.T) {
	headers := map[string]string{
		"X-Okapi-Tenant": "mytenant",
		"X-Other":        "myother",
		"User-Agent":     vcs.GetSignature(),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		for k, v := range headers {
			assert.Equal(t, v, r.Header.Get(k))
		}
		msg := &iso18626.ISO18626Message{}
		buf, err := xml.Marshal(msg)
		assert.NoError(t, err)
		_, err = w.Write(buf)
		assert.NoError(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 0, 0*time.Second)

	msg := &iso18626.ISO18626Message{}
	peer := ill_db.Peer{
		Url:         server.URL,
		HttpHeaders: headers,
	}
	_, err := client.SendHttpPost(&peer, msg)
	assert.NoError(t, err)
}

func TestGetPeerNameAndAddress(t *testing.T) {
	jsonString := "{\"id\":\"758f6cc5-0a5a-5d34-922a-8e981d7902f5\",\"name\":\"ACTLegislativeAssemblyLibrary\",\"description\":\"act\",\"type\":\"institution\",\"email\":\"LALibrary@parliament.act.gov.au\",\"symbols\":[{\"id\":\"f4ea1bf8-8278-5c0f-8e0f-9db9b35fa3cf\",\"symbol\":\"AU-ACT\",\"authority\":\"ISIL\"}],\"endpoints\":[{\"id\":\"e7c5c06b-d1ce-5294-a07c-ae13522ed0e3\",\"entry\":\"758f6cc5-0a5a-5d34-922a-8e981d7902f5\",\"name\":\"ACTISO18626Service\",\"type\":\"ISO18626\",\"address\":\"https://act-okapi.au.reshare.indexdata.com/_/invoke/tenant/act/rs/externalApi/iso18626\"}],\"networks\":[{\"id\":\"b35cf98c-2341-5f64-8a7c-a0e6343413ff\",\"name\":\"NSW&ACTGovt&Arts\",\"consortium\":\"d5ab4617-d503-588e-802c-df8d25bb411f\",\"priority\":1}],\"tiers\":[{\"id\":\"6bb0026f-8127-528f-bb39-30d8d90e47bd\",\"name\":\"ReciprocalPeertoPeer-CoreLoan\",\"consortium\":\"d5ab4617-d503-588e-802c-df8d25bb411f\",\"type\":\"Loan\",\"level\":\"Standard\",\"cost\":0.0}],\"addresses\":[{\"id\":\"1ef3063a-8ec6-587e-bbc3-fdb59024f471\",\"entry\":\"758f6cc5-0a5a-5d34-922a-8e981d7902f5\",\"type\":\"Shipping\",\"addressComponents\":[{\"id\":\"06f2dbed-6e86-5627-9305-1e0dfc773521\",\"address\":\"1ef3063a-8ec6-587e-bbc3-fdb59024f471\",\"type\":\"Thoroughfare\",\"value\":\"196LondonCircuit\"},{\"id\":\"e69b518d-1b03-528e-a1fb-8dc92385aff5\",\"address\":\"1ef3063a-8ec6-587e-bbc3-fdb59024f471\",\"type\":\"Locality\",\"value\":\"Canberra\"},{\"id\":\"8a585d89-f37d-5827-bfac-e7cfb3cdbbb5\",\"address\":\"1ef3063a-8ec6-587e-bbc3-fdb59024f471\",\"type\":\"AdministrativeArea\",\"value\":\"ACT\"},{\"id\":\"b7883220-3110-57c0-9895-61abbbe0d830\",\"address\":\"1ef3063a-8ec6-587e-bbc3-fdb59024f471\",\"type\":\"PostalCode\",\"value\":\"2601\"},{\"id\":\"af5b9560-4562-52a8-bdfc-100191a712ca\",\"address\":\"1ef3063a-8ec6-587e-bbc3-fdb59024f471\",\"type\":\"CountryCode\",\"value\":\"AUS\"}]}]}"
	var data map[string]any
	err := json.Unmarshal([]byte(jsonString), &data)
	assert.Nil(t, err)
	peer := ill_db.Peer{
		Name:       "ACTLegislativeAssemblyLibrary",
		CustomData: data,
	}
	name, agencyId, address, email := getPeerInfo(&peer, "")
	assert.Equal(t, "ACTLegislativeAssemblyLibrary", name)
	assert.Equal(t, "", agencyId.AgencyIdValue)
	assert.Equal(t, "", agencyId.AgencyIdType.Text)
	assert.Equal(t, "196LondonCircuit", address.Line1)
	assert.Equal(t, "Canberra", address.Locality)
	assert.Equal(t, "2601", address.PostalCode)
	assert.Equal(t, "ACT", address.Region.Text)
	assert.Equal(t, "AUS", address.Country.Text)
	assert.Equal(t, "LALibrary@parliament.act.gov.au", email.ElectronicAddressData)

	name, agencyId, address, email = getPeerInfo(&peer, "ISIL:ACT")
	assert.Equal(t, "ACTLegislativeAssemblyLibrary (ISIL:ACT)", name)
	assert.Equal(t, "ACT", agencyId.AgencyIdValue)
	assert.Equal(t, "ISIL", agencyId.AgencyIdType.Text)
	assert.Equal(t, "196LondonCircuit", address.Line1)
	assert.Equal(t, "LALibrary@parliament.act.gov.au", email.ElectronicAddressData)
}

func TestPopulateRequesterInfo(t *testing.T) {
	message := iso18626.ISO18626Message{
		Request: &iso18626.Request{},
	}
	name := "Requester 1"
	address := iso18626.PhysicalAddress{
		Line1: "Home 1",
	}
	email := iso18626.ElectronicAddress{
		ElectronicAddressData: "me@box.com",
		ElectronicAddressType: iso18626.TypeSchemeValuePair{
			Text: string(iso18626.ElectronicAddressTypeEmail),
		},
	}
	populateRequesterInfo(&message, name, address, email)

	assert.Equal(t, name, message.Request.RequestingAgencyInfo.Name)
	assert.Equal(t, address.Line1, message.Request.RequestingAgencyInfo.Address[0].PhysicalAddress.Line1)
	assert.Equal(t, email.ElectronicAddressData, message.Request.RequestingAgencyInfo.Address[1].ElectronicAddress.ElectronicAddressData)
	// does not override if already set
	populateRequesterInfo(&message, "other", iso18626.PhysicalAddress{Line2: "Home 2"}, iso18626.ElectronicAddress{ElectronicAddressData: "me2@box.com"})
	assert.Equal(t, name, message.Request.RequestingAgencyInfo.Name)
	assert.Equal(t, address.Line1, message.Request.RequestingAgencyInfo.Address[0].PhysicalAddress.Line1)
	assert.Equal(t, email.ElectronicAddressData, message.Request.RequestingAgencyInfo.Address[1].ElectronicAddress.ElectronicAddressData)
}

func TestPopulateDeliveryAddress(t *testing.T) {
	message := iso18626.ISO18626Message{
		Request: &iso18626.Request{},
	}
	address := iso18626.PhysicalAddress{
		Line1: "Home 1",
	}
	email := iso18626.ElectronicAddress{
		ElectronicAddressData: "me@box.com",
		ElectronicAddressType: iso18626.TypeSchemeValuePair{
			Text: string(iso18626.ElectronicAddressTypeEmail),
		},
	}
	populateDeliveryAddress(&message, address, email)
	assert.Equal(t, 2, len(message.Request.RequestedDeliveryInfo))
	assert.Equal(t, address.Line1, message.Request.RequestedDeliveryInfo[0].Address.PhysicalAddress.Line1)
	assert.Equal(t, email.ElectronicAddressData, message.Request.RequestedDeliveryInfo[1].Address.ElectronicAddress.ElectronicAddressData)
	// does override if already set
	populateDeliveryAddress(&message, iso18626.PhysicalAddress{Line2: "Home 2"}, iso18626.ElectronicAddress{ElectronicAddressData: "me2@box.com"})
	assert.Equal(t, 2, len(message.Request.RequestedDeliveryInfo))
	assert.Equal(t, address.Line1, message.Request.RequestedDeliveryInfo[0].Address.PhysicalAddress.Line1)
	assert.Equal(t, email.ElectronicAddressData, message.Request.RequestedDeliveryInfo[1].Address.ElectronicAddress.ElectronicAddressData)
}

func TestPopulateDeliveryAddressPatron(t *testing.T) {
	yes := iso18626.TypeYesNoY
	message := iso18626.ISO18626Message{
		Request: &iso18626.Request{
			PatronInfo: &iso18626.PatronInfo{
				GivenName:    "Patron 1",
				SendToPatron: &yes,
				Address: []iso18626.Address{
					{
						PhysicalAddress: &iso18626.PhysicalAddress{
							Line1: "Patron Home 1",
						},
					},
					{
						ElectronicAddress: &iso18626.ElectronicAddress{
							ElectronicAddressData: "patron@email.com",
							ElectronicAddressType: iso18626.TypeSchemeValuePair{
								Text: string(iso18626.ElectronicAddressTypeEmail),
							},
						},
					},
				},
			},
		},
	}
	address := iso18626.PhysicalAddress{
		Line1: "Home 1",
	}
	email := iso18626.ElectronicAddress{
		ElectronicAddressData: "me@box.com",
		ElectronicAddressType: iso18626.TypeSchemeValuePair{
			Text: string(iso18626.ElectronicAddressTypeEmail),
		},
	}
	populateDeliveryAddress(&message, address, email)
	assert.Equal(t, 2, len(message.Request.RequestedDeliveryInfo))
	assert.Equal(t, message.Request.PatronInfo.Address[0].PhysicalAddress.Line1, message.Request.RequestedDeliveryInfo[0].Address.PhysicalAddress.Line1)
	assert.Equal(t, message.Request.PatronInfo.Address[1].ElectronicAddress.ElectronicAddressData, message.Request.RequestedDeliveryInfo[1].Address.ElectronicAddress.ElectronicAddressData)
}

func TestPopulateSupplierAddress(t *testing.T) {
	message := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{},
	}
	name := "Requester 1"
	address := iso18626.PhysicalAddress{
		Line1: "Home 1",
	}
	agencyId := iso18626.TypeAgencyId{
		AgencyIdValue: "SUP1",
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: "ISIL",
		},
	}
	populateReturnAddress(&message, name, agencyId, address)
	assert.Equal(t, "SUP1", message.SupplyingAgencyMessage.ReturnInfo.ReturnAgencyId.AgencyIdValue)
	assert.Equal(t, "ISIL", message.SupplyingAgencyMessage.ReturnInfo.ReturnAgencyId.AgencyIdType.Text)
	assert.Equal(t, name, message.SupplyingAgencyMessage.ReturnInfo.Name)
	assert.Equal(t, address.Line1, message.SupplyingAgencyMessage.ReturnInfo.PhysicalAddress.Line1)

	name = "Requester 2"
	address = iso18626.PhysicalAddress{
		Line1: "Home 2",
	}
	agencyId = iso18626.TypeAgencyId{
		AgencyIdValue: "SUP2",
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: "ISIL",
		},
	}
	// Don't override if already set
	populateReturnAddress(&message, name, agencyId, address)
	assert.Equal(t, "SUP1", message.SupplyingAgencyMessage.ReturnInfo.ReturnAgencyId.AgencyIdValue)
	assert.Equal(t, "ISIL", message.SupplyingAgencyMessage.ReturnInfo.ReturnAgencyId.AgencyIdType.Text)
	assert.Equal(t, "Requester 1", message.SupplyingAgencyMessage.ReturnInfo.Name)
	assert.Equal(t, "Home 1", message.SupplyingAgencyMessage.ReturnInfo.PhysicalAddress.Line1)
}

func TestPopulateSupplierInfo(t *testing.T) {
	message := iso18626.ISO18626Message{
		Request: &iso18626.Request{},
	}
	name := "Supplier 1"
	address := iso18626.PhysicalAddress{
		Line1: "Home 1",
	}
	agencyId := iso18626.TypeAgencyId{
		AgencyIdValue: "SUP1",
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: "ISIL",
		},
	}
	desc := shim.RETURN_ADDRESS_BEGIN + "\n" + name + "\n" + address.Line1 + "\n" + shim.RETURN_ADDRESS_END + "\n"
	populateSupplierInfo(&message, name, agencyId, address)
	assert.Equal(t, 1, len(message.Request.SupplierInfo))
	assert.Equal(t, "SUP1", message.Request.SupplierInfo[0].SupplierCode.AgencyIdValue)
	assert.Equal(t, "ISIL", message.Request.SupplierInfo[0].SupplierCode.AgencyIdType.Text)
	assert.Equal(t, desc, message.Request.SupplierInfo[0].SupplierDescription)
	assert.Contains(t, message.Request.SupplierInfo[0].SupplierDescription, name)
	assert.Contains(t, message.Request.SupplierInfo[0].SupplierDescription, address.Line1)

	name2 := "Supplier 2"
	address2 := iso18626.PhysicalAddress{
		Line1: "Home 2",
	}
	agencyId2 := iso18626.TypeAgencyId{
		AgencyIdValue: "SUP2",
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: "ISIL",
		},
	}
	// Don't override if already set
	populateSupplierInfo(&message, name2, agencyId2, address2)
	assert.Equal(t, 1, len(message.Request.SupplierInfo))
	assert.Equal(t, "SUP1", message.Request.SupplierInfo[0].SupplierCode.AgencyIdValue)
	assert.Equal(t, "ISIL", message.Request.SupplierInfo[0].SupplierCode.AgencyIdType.Text)
	assert.Contains(t, message.Request.SupplierInfo[0].SupplierDescription, name)
	assert.Contains(t, message.Request.SupplierInfo[0].SupplierDescription, address.Line1)
}

func TestValidateReason(t *testing.T) {
	// Valid reason
	reason := guessReason(iso18626.TypeReasonForMessageRequestResponse, string(ill_db.RequestAction), "", iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageRequestResponse, string(ill_db.RequestAction), string(iso18626.TypeStatusExpectToSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason(iso18626.TypeReasonForMessageStatusChange, string(ill_db.RequestAction), "", iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageStatusChange, string(ill_db.RequestAction), string(iso18626.TypeStatusExpectToSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason(iso18626.TypeReasonForMessageNotification, string(ill_db.RequestAction), "", iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason(iso18626.TypeReasonForMessageNotification, string(ill_db.RequestAction), string(iso18626.TypeStatusExpectToSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason(iso18626.TypeReasonForMessageNotification, string(ill_db.RequestAction), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason("", string(ill_db.RequestAction), "", iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = guessReason("", string(ill_db.RequestAction), string(iso18626.TypeStatusExpectToSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason("", string(iso18626.TypeActionNotification), "", iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = guessReason("", string(iso18626.TypeActionNotification), string(iso18626.TypeStatusExpectToSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionCancel), "", iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageCancelResponse, string(iso18626.TypeActionCancel), "", iso18626.TypeStatusCancelled)
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageNotification, string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason(iso18626.TypeReasonForMessageNotification, string(iso18626.TypeActionRenew), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason(iso18626.TypeReasonForMessageNotification, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason("", string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusUnfilled)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason("", string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionRenew), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRenewResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusRequestResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = guessReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionRenew), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageRenewResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusExpectToSupply)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusRequestResponse, reason)
	reason = guessReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusUnfilled)
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = guessReason("", string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply), iso18626.TypeStatusUnfilled)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusRequestResponse, reason)
}

func TestPopulateVendorInNote(t *testing.T) {
	message := iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{},
	}
	message.SupplyingAgencyMessage.MessageInfo.Note = ""
	populateVendor(message.SupplyingAgencyMessage, "Alma")
	assert.Equal(t, message.SupplyingAgencyMessage.MessageInfo.Note, "Vendor: Alma")
	message.SupplyingAgencyMessage.MessageInfo.Note = "some note"
	populateVendor(message.SupplyingAgencyMessage, "Alma")
	assert.Equal(t, message.SupplyingAgencyMessage.MessageInfo.Note, "Vendor: Alma, some note")
	message.SupplyingAgencyMessage.MessageInfo.Note = "#special note#"
	populateVendor(message.SupplyingAgencyMessage, "ReShare")
	assert.Equal(t, message.SupplyingAgencyMessage.MessageInfo.Note, "Vendor: ReShare#special note#")
}

func TestReadTransactionContextSuccess(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(mocks.MockIllRepositorySuccess), 1, 0*time.Second)
	event := events.Event{IllTransactionID: "1"}
	trCtx, err := client.readTransactionContext(appCtx, event, true)
	assert.NoError(t, err)
	assert.NotNil(t, trCtx.transaction)
	assert.NotNil(t, trCtx.requester)
	assert.NotNil(t, trCtx.selectedSupplier)
	assert.NotNil(t, trCtx.selectedPeer)
	assert.Equal(t, event, trCtx.event)
}

func TestReadTransactionContextError(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(mocks.MockIllRepositoryError), 1, 0*time.Second)
	event := events.Event{IllTransactionID: "1"}
	trCtx, err := client.readTransactionContext(appCtx, event, true)
	assert.Equal(t, FailedToReadTransaction+": DB error", err.Error())
	assert.Nil(t, trCtx.transaction)
	assert.Nil(t, trCtx.requester)
	assert.Nil(t, trCtx.selectedSupplier)
	assert.Nil(t, trCtx.selectedPeer)
	assert.Equal(t, event, trCtx.event)
}

func createSupplyingAgencyMessageEvent(notification bool) events.Event {
	reason := iso18626.TypeReasonForMessageStatusChange
	if notification {
		reason = iso18626.TypeReasonForMessageNotification
	}
	return events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: &iso18626.ISO18626Message{
					SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
						Header: iso18626.Header{
							SupplyingAgencyId: iso18626.TypeAgencyId{
								AgencyIdType: iso18626.TypeSchemeValuePair{
									Text: "isil",
								},
								AgencyIdValue: "sup1",
							},
						},
						MessageInfo: iso18626.MessageInfo{
							ReasonForMessage: reason,
						},
					},
				},
			},
		},
	}
}

func createTransactionContext(event events.Event, selectedSupplier *ill_db.LocatedSupplier, selectedPeer *ill_db.Peer, brokerMode common.BrokerMode) transactionContext {
	if selectedPeer != nil {
		selectedPeer.BrokerMode = string(brokerMode)
	}
	return transactionContext{
		transaction: &ill_db.IllTransaction{
			RequesterSymbol: getPgText("ISIL:REQ"),
		},
		requester: &ill_db.Peer{
			Name:       "Requester",
			BrokerMode: string(brokerMode),
		},
		selectedSupplier: selectedSupplier,
		selectedPeer:     selectedPeer,
		event:            event,
	}
}

func getPgText(text string) pgtype.Text {
	return pgtype.Text{
		String: text,
		Valid:  true,
	}
}

type MockIllRepositorySkippedSup struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockIllRepositorySkippedSup) GetLocatedSupplierByIllTransactionAndSymbol(_ common.ExtendedContext, illTransactionId string, symbol string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		ID:               uuid.NewString(),
		IllTransactionID: illTransactionId,
		SupplierSymbol:   symbol,
		SupplierID:       "skipped",
		SupplierStatus:   ill_db.SupplierStateSkippedPg,
	}, nil
}

func TestDetermineMessageTarget_handleSkippedSupplierNotification_BrokerModeTransparent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	trCtx := createTransactionContext(event, nil, nil, common.BrokerModeTransparent)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, "skipped", msgTarget.supplier.SupplierID)
}

func TestDetermineMessageTarget_handleSkippedSupplierNotification_BrokerModeOpaque(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	trCtx := createTransactionContext(event, nil, nil, common.BrokerModeOpaque)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, "ignored notification from skipped supplier isil:sup1 due to requester mode opaque", *msgTarget.problemDetails)
}

func TestDetermineMessageTarget_handleSkippedSupplierNotification_Error(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(mocks.MockIllRepositorySuccess), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	trCtx := createTransactionContext(event, nil, nil, common.BrokerModeOpaque)

	_, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Equal(t, "supplier isil:sup1 is not in skipped state", err.Error())
}

func TestDetermineMessageTarget_handleNoSelectedSupplier_Unfilled(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(mocks.MockIllRepositorySuccess), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(false)
	trCtx := createTransactionContext(event, nil, nil, common.BrokerModeOpaque)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, iso18626.TypeStatusUnfilled, msgTarget.status)
	assert.False(t, msgTarget.firstMessage)
}

func TestDetermineMessageTargetWithSupplier_handleSkippedSupplierNotification_BrokerModeTransparent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	trCtx := createTransactionContext(event, &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup2"}, &ill_db.Peer{}, common.BrokerModeTransparent)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, "skipped", msgTarget.supplier.SupplierID)
}

func TestDetermineMessageTargetWithSupplier_handleSkippedSupplierNotification_BrokerModeOpaque(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	trCtx := createTransactionContext(event, &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup2"}, &ill_db.Peer{}, common.BrokerModeOpaque)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, "ignored notification from skipped supplier isil:sup1 due to requester mode opaque", *msgTarget.problemDetails)
}

func TestDetermineMessageTargetWithSupplier_handleSkippedSupplierNotification_Error(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(mocks.MockIllRepositorySuccess), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	trCtx := createTransactionContext(event, &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup2"}, &ill_db.Peer{}, common.BrokerModeOpaque)

	_, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Equal(t, "supplier isil:sup1 is not in skipped state", err.Error())
}

func TestDetermineMessageTarget_handleSelectedSupplier_StatusLoaned(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1", LastStatus: getPgText(string(iso18626.TypeStatusLoaned))}
	supPeer := &ill_db.Peer{}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeOpaque)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, sup, msgTarget.supplier)
	assert.Equal(t, supPeer, msgTarget.peer)
	assert.Equal(t, iso18626.TypeStatusLoaned, msgTarget.status)
	assert.False(t, msgTarget.firstMessage)
}

func TestDetermineMessageTarget_handleSelectedSupplier_StatusInvalid(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1", LastStatus: getPgText("invalid")}
	supPeer := &ill_db.Peer{}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeOpaque)

	_, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Equal(t, "failed to resolve status for value: invalid", err.Error())
}

func TestDetermineMessageTarget_handleSelectedSupplier_NoStatus(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1"}
	supPeer := &ill_db.Peer{}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeOpaque)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, sup, msgTarget.supplier)
	assert.Nil(t, msgTarget.peer)
	assert.Equal(t, iso18626.TypeStatusExpectToSupply, msgTarget.status)
	assert.True(t, msgTarget.firstMessage)
}

func TestDetermineMessageTarget_handleSelectedSupplier_NoStatus_NoMessage_BrokerModeOpaque(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	event.EventData.IncomingMessage = nil
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1"}
	supPeer := &ill_db.Peer{}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeOpaque)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, "broker does not send ExpectToSupply in mode opaque", msgTarget.note)
	assert.Nil(t, msgTarget.peer)
	assert.Nil(t, msgTarget.supplier)
	assert.False(t, msgTarget.firstMessage)
}
func TestDetermineMessageTarget_handleSelectedSupplier_NoStatus_NoMessage_BrokerModeTransparent(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)
	event := createSupplyingAgencyMessageEvent(true)
	event.EventData.IncomingMessage = nil
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1"}
	supPeer := &ill_db.Peer{}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeTransparent)

	msgTarget, err := client.determineMessageTarget(appCtx, trCtx)

	assert.Nil(t, err)
	assert.Equal(t, sup, msgTarget.supplier)
	assert.Nil(t, msgTarget.peer)
	assert.Equal(t, iso18626.TypeStatusExpectToSupply, msgTarget.status)
	assert.True(t, msgTarget.firstMessage)
}

func TestBuildSupplyingAgencyMessage(t *testing.T) {
	event := createSupplyingAgencyMessageEvent(true)
	event.EventData.IncomingMessage.SupplyingAgencyMessage.DeliveryInfo = &iso18626.DeliveryInfo{
		ItemId: "testId1",
	}
	event.EventData.IncomingMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1"}
	supPeer := &ill_db.Peer{
		Name:   "isil:sup1",
		Vendor: string(common.VendorAlma),
	}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeTransparent)
	msgTarget := messageTarget{
		status:       iso18626.TypeStatusLoaned,
		firstMessage: true,
		supplier:     sup,
		peer:         supPeer,
	}
	message := createSupplyingAgencyMessage(trCtx, &msgTarget).SupplyingAgencyMessage
	assert.Equal(t, "testId1", message.DeliveryInfo.ItemId)
	assert.Equal(t, "sup1", message.Header.SupplyingAgencyId.AgencyIdValue)
	assert.Equal(t, "REQ", message.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, message.MessageInfo.ReasonForMessage)
	assert.Equal(t, "Vendor: Alma", message.MessageInfo.Note)
	assert.Equal(t, iso18626.TypeStatusLoaned, message.StatusInfo.Status)
	assert.Equal(t, "isil:sup1 (isil:sup1)", message.ReturnInfo.Name)
}

func TestBuildSupplyingAgencyMessage_NoIncomingMessage(t *testing.T) {
	event := createSupplyingAgencyMessageEvent(true)
	event.EventData.IncomingMessage = nil
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1"}
	supPeer := &ill_db.Peer{
		Name:   "isil:sup1",
		Vendor: string(common.VendorAlma),
	}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeTransparent)
	msgTarget := messageTarget{
		status:       iso18626.TypeStatusLoaned,
		firstMessage: true,
		supplier:     sup,
		peer:         supPeer,
	}
	message := createSupplyingAgencyMessage(trCtx, &msgTarget).SupplyingAgencyMessage
	assert.Nil(t, message.DeliveryInfo)
	assert.Equal(t, "sup1", message.Header.SupplyingAgencyId.AgencyIdValue)
	assert.Equal(t, "REQ", message.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, message.MessageInfo.ReasonForMessage)
	assert.Equal(t, "Vendor: Alma", message.MessageInfo.Note)
	assert.Equal(t, iso18626.TypeStatusLoaned, message.StatusInfo.Status)
	assert.Equal(t, "isil:sup1 (isil:sup1)", message.ReturnInfo.Name)
}
func TestSendAndUpdateStatus_DontSend(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	event := createSupplyingAgencyMessageEvent(true)
	event.EventData.CustomData = map[string]any{"doNotSend": true}
	trCtx := createTransactionContext(event, nil, nil, common.BrokerModeTransparent)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)

	status, resData := client.sendAndUpdateStatus(appCtx, trCtx, event.EventData.IncomingMessage)

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resData.OutgoingMessage)
	doNotSend, ok := resData.CustomData["doNotSend"].(bool)
	assert.True(t, doNotSend)
	assert.True(t, ok)
}

func TestCreateRequestMessage(t *testing.T) {
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1", LocalID: getPgText("id1")}
	supPeer := &ill_db.Peer{
		Name:   "isil:sup1",
		Vendor: string(common.VendorAlma),
	}
	trCtx := createTransactionContext(events.Event{}, sup, supPeer, common.BrokerModeTransparent)

	message, action := createRequestMessage(trCtx)

	assert.Equal(t, iso18626.TypeAction(ill_db.RequestAction), action)
	assert.Equal(t, "sup1", message.Request.Header.SupplyingAgencyId.AgencyIdValue)
	assert.Equal(t, "REQ", message.Request.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "id1", message.Request.BibliographicInfo.SupplierUniqueRecordId)
	assert.Equal(t, "#RETURN_TO#\nisil:sup1 (isil:sup1)\n#RT_END#\n", message.Request.SupplierInfo[0].SupplierDescription)
	assert.Equal(t, "Requester (ISIL:REQ)", message.Request.RequestingAgencyInfo.Name)
}

func TestCreateRequestingAgencyMessage(t *testing.T) {
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1", LocalID: getPgText("id1")}
	supPeer := &ill_db.Peer{
		Name:   "isil:sup1",
		Vendor: string(common.VendorAlma),
	}
	event := events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				IncomingMessage: &iso18626.ISO18626Message{
					RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
						Note: "This is note",
					},
				},
			},
		},
	}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeTransparent)
	trCtx.transaction.LastRequesterAction = getPgText("Received")

	message, action, errorMessage := createRequestingAgencyMessage(trCtx)

	assert.Equal(t, iso18626.TypeActionReceived, action)
	assert.NoError(t, errorMessage)
	assert.Equal(t, "sup1", message.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
	assert.Equal(t, "REQ", message.RequestingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, iso18626.TypeActionReceived, message.RequestingAgencyMessage.Action)
	assert.Equal(t, "This is note", message.RequestingAgencyMessage.Note)
}

func TestCreateRequestingAgencyMessage_error(t *testing.T) {
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1", LocalID: getPgText("id1")}
	supPeer := &ill_db.Peer{
		Name:   "isil:sup1",
		Vendor: string(common.VendorAlma),
	}
	event := events.Event{}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeTransparent)
	trCtx.transaction.LastRequesterAction = getPgText("NotFound")

	_, action, errorMessage := createRequestingAgencyMessage(trCtx)

	assert.Equal(t, iso18626.TypeAction(""), action)
	assert.Equal(t, "failed to resolve action for value: NotFound", errorMessage.Error())
}

func TestSendAndUpdateSupplier_DontSend(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	event := createSupplyingAgencyMessageEvent(true)
	event.EventData.CustomData = map[string]any{"doNotSend": true}
	sup := &ill_db.LocatedSupplier{SupplierSymbol: "isil:sup1", LocalID: getPgText("id1")}
	supPeer := &ill_db.Peer{
		Name:   "isil:sup1",
		Vendor: string(common.VendorAlma),
	}
	trCtx := createTransactionContext(event, sup, supPeer, common.BrokerModeTransparent)
	client := CreateIso18626Client(new(events.PostgresEventBus), new(MockIllRepositorySkippedSup), 1, 0*time.Second)

	status, resData := client.sendAndUpdateSupplier(appCtx, trCtx, event.EventData.IncomingMessage, "Received")

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resData.OutgoingMessage)
	doNotSend, ok := resData.CustomData["doNotSend"].(bool)
	assert.True(t, doNotSend)
	assert.True(t, ok)
}

func TestBlockUnfilled(t *testing.T) {
	requester := ill_db.Peer{BrokerMode: string(common.BrokerModeTransparent)}
	trCtx := transactionContext{event: events.Event{
		EventData: events.EventData{},
	}, requester: &requester}
	assert.False(t, blockUnfilled(trCtx))

	trCtx.event.EventData.IncomingMessage = &iso18626.ISO18626Message{}
	assert.False(t, blockUnfilled(trCtx))

	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	assert.False(t, blockUnfilled(trCtx))

	messageInfo := iso18626.MessageInfo{
		Note: "Will not deliver",
		ReasonUnfilled: &iso18626.TypeSchemeValuePair{
			Text: "Not available",
		},
	}
	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage.MessageInfo = messageInfo
	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage.StatusInfo = iso18626.StatusInfo{
		Status: iso18626.TypeStatusUnfilled,
	}
	assert.False(t, blockUnfilled(trCtx))

	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage.StatusInfo.Status = iso18626.TypeStatusLoaned
	assert.False(t, blockUnfilled(trCtx))

	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage.StatusInfo.Status = iso18626.TypeStatusUnfilled
	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage.MessageInfo = iso18626.MessageInfo{}
	assert.True(t, blockUnfilled(trCtx))

	trCtx.event.EventData.IncomingMessage.SupplyingAgencyMessage.MessageInfo.Note = "Will not deliver"
	assert.False(t, blockUnfilled(trCtx))

	trCtx.requester.BrokerMode = string(common.BrokerModeOpaque)
	assert.True(t, blockUnfilled(trCtx))
}
