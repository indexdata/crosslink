package client

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, 0*time.Second)
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := client.createMessageHeader(illTrans, &sup, true, string(common.BrokerModeTransparent))
	assert.Equal(t, "REQ", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := client.createMessageHeader(illTrans, &sup, false, string(common.BrokerModeTransparent))
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestCreateMessageHeaderOpaque(t *testing.T) {
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, 0*time.Second)
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := client.createMessageHeader(illTrans, &sup, true, string(common.BrokerModeOpaque))
	assert.Equal(t, "BROKER", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := client.createMessageHeader(illTrans, &sup, false, string(common.BrokerModeOpaque))
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "BROKER", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestCreateMessageHeaderTranslucent(t *testing.T) {
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, 0*time.Second)
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := client.createMessageHeader(illTrans, &sup, true, string(common.BrokerModeTranslucent))
	assert.Equal(t, "BROKER", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := client.createMessageHeader(illTrans, &sup, false, string(common.BrokerModeTranslucent))
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
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, 0*time.Second)
	// Valid reason
	reason := client.validateReason(iso18626.TypeReasonForMessageRequestResponse, ill_db.RequestAction, "")
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageRequestResponse, ill_db.RequestAction, string(iso18626.TypeStatusExpectToSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageStatusChange, ill_db.RequestAction, "")
	assert.Equal(t, iso18626.TypeReasonForMessageRequestResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageStatusChange, ill_db.RequestAction, string(iso18626.TypeStatusExpectToSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageNotification, ill_db.RequestAction, "")
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageNotification, ill_db.RequestAction, string(iso18626.TypeStatusExpectToSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageNotification, ill_db.RequestAction, string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageNotification, string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageNotification, string(iso18626.TypeActionRenew), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageNotification, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageNotification, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionRenew), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageRenewResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageStatusChange, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageStatusRequestResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionCancel), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionRenew), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageRenewResponse, reason)
	reason = client.validateReason(iso18626.TypeReasonForMessageRequestResponse, string(iso18626.TypeActionStatusRequest), string(iso18626.TypeStatusWillSupply))
	assert.Equal(t, iso18626.TypeReasonForMessageStatusRequestResponse, reason)
}
