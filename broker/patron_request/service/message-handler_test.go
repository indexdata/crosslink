package prservice

import (
	"errors"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetPatronRequestId(t *testing.T) {
	msg := iso18626.ISO18626Message{
		Request: &iso18626.Request{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "req-id-1",
				SupplyingAgencyRequestId:  "sam-id-1",
			},
		},
	}
	assert.Equal(t, "req-id-1", getPatronRequestId(msg))

	msg = iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "ram-id-1",
				SupplyingAgencyRequestId:  "sam-id-1",
			},
		},
	}
	assert.Equal(t, "sam-id-1", getPatronRequestId(msg))

	msg = iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "ram-id-1",
				SupplyingAgencyRequestId:  "sam-id-1",
			},
		},
	}
	assert.Equal(t, "ram-id-1", getPatronRequestId(msg))
}

func TestHandleMessageNoMessage(t *testing.T) {
	handler := CreatePatronRequestMessageHandler(*new(pr_db.PrRepo), *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	mes, err := handler.HandleMessage(appCtx, nil)

	assert.Nil(t, mes)
	assert.Equal(t, "cannot process nil message", err.Error())
}

func TestHandleMessageFetchPRError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("db error"))

	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	mes, err := handler.HandleMessage(appCtx, &iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: patronRequestId,
			},
		},
	})

	assert.Nil(t, mes)
	assert.Equal(t, "db error", err.Error())
}

func TestHandleMessageFetchEventError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: "error"}, nil)

	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	mes, err := handler.HandleMessage(appCtx, &iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: patronRequestId,
			},
		},
	})

	assert.Nil(t, mes)
	assert.Equal(t, "event bus error", err.Error())
}

func TestHandlePatronRequestMessage(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{})
	assert.Equal(t, events.EventStatusError, status)
	assert.Nil(t, resp)
	assert.Equal(t, "cannot process message without content", err.Error())

	status, resp, err = handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{Request: &iso18626.Request{}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Nil(t, resp)
	assert.Equal(t, "request handling is not implemented yet", err.Error())

	status, resp, err = handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Nil(t, resp)
	assert.Equal(t, "requesting agency message handling is not implemented yet", err.Error())

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("db error"))
	status, resp, err = handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{Header: iso18626.Header{RequestingAgencyRequestId: patronRequestId}}})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, "could not find patron request: db error", resp.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
}

func TestHandleSupplyingAgencyMessageNoSupplier(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetPeerBySymbol", "ISIL:SUP1").Return(ill_db.Peer{}, errors.New("db error"))
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), mockIllRepo, *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusExpectToSupply},
	})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, "could not find supplier: db error", resp.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
}

func TestHandleSupplyingAgencyMessageExpectToSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetPeerBySymbol", "ISIL:SUP1").Return(ill_db.Peer{ID: "peer1"}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), mockIllRepo, *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusExpectToSupply},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NoError(t, err)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateSupplierLocated, mockPrRepo.savedPr.State)
}

func TestHandleSupplyingAgencyMessageWillSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateWillSupply, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageWillSupplyCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply},
		MessageInfo: iso18626.MessageInfo{
			Note: RESHARE_ADD_LOAN_CONDITION + " some comment",
		},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateConditionPending, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageLoaned(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateShipped, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageLoanCompleted(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusLoanCompleted},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCompleted, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageUnfilled(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusUnfilled},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateUnfilled, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageCancelled(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCancelled, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageNoImplemented(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusEmpty},
	})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "status change no allowed", resp.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
	assert.Equal(t, "status change no allowed", err.Error())
}
