package handler

import (
	"context"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"testing"

	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
)

func TestGetSupplierSymbol(t *testing.T) {
	header := &iso18626.Header{
		SupplyingAgencyId: iso18626.TypeAgencyId{
			AgencyIdType: iso18626.TypeSchemeValuePair{
				Text: "ISIL",
			},
			AgencyIdValue: "12345",
		},
	}
	symbol := getSupplierSymbol(header)
	assert.Equal(t, "ISIL:12345", symbol)
	header.SupplyingAgencyId.AgencyIdType.Text = ""
	symbol = getSupplierSymbol(header)
	assert.Equal(t, "", symbol)
	header.SupplyingAgencyId.AgencyIdType.Text = "ISIL"
	header.SupplyingAgencyId.AgencyIdValue = ""
	symbol = getSupplierSymbol(header)
	assert.Equal(t, "", symbol)
}

func TestApplyRequesterShimError(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(mocks.MockIllRepositoryError)
	eventData := events.EventData{}
	eVal, err := applyRequesterShim(appCtx, mockRepo, "1", &iso18626.ISO18626Message{}, &eventData, nil)
	assert.Equal(t, eVal, ReqAgencyNotFound)
	assert.Equal(t, "DB error", err.Error())
}

func TestApplyRequesterShim(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(mocks.MockIllRepositorySuccess)
	eventData := events.EventData{}
	_, err := applyRequesterShim(appCtx, mockRepo, "1", &iso18626.ISO18626Message{}, &eventData, nil)
	assert.NoError(t, err, "should not have DB error")
	assert.NotNil(t, eventData.IncomingMessage)
	assert.NotNil(t, eventData.CustomData[ORIGINAL_INCOMING_MESSAGE])
}

func TestApplyRequesterShimAlma(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	mockRepo := new(MockIllRepositorySuccessAlma)
	eventData := events.EventData{}
	message := iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: "ISIL",
					},
					AgencyIdValue: "BROKER",
				},
			},
			Action: iso18626.TypeActionNotification,
			Note:   "ReJeCT",
		},
	}
	_, err := applyRequesterShim(appCtx, mockRepo, "1", &message, &eventData, &ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP1"})
	assert.NoError(t, err, "should not have DB error")
	assert.NotNil(t, eventData.IncomingMessage)
	assert.Equal(t, "SUP1", eventData.IncomingMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
	assert.NotNil(t, eventData.CustomData[ORIGINAL_INCOMING_MESSAGE])
	assert.Equal(t, "BROKER", eventData.CustomData[ORIGINAL_INCOMING_MESSAGE].(*iso18626.ISO18626Message).RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}

type MockIllRepositorySuccessAlma struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockIllRepositorySuccessAlma) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID:     id,
		Vendor: string(common.VendorAlma),
	}, nil
}
