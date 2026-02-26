package prservice

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestGetPatronRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", "req-id-1").Return(pr_db.PatronRequest{ID: "req-id-1"}, nil)
	mockPrRepo.On("GetPatronRequestById", "sam-id-1").Return(pr_db.PatronRequest{ID: "sam-id-1"}, nil)
	mockPrRepo.On("GetPatronRequestBySupplierSymbolAndRequesterReqId", "ISIL:SUP1", "req-id-1").Return(pr_db.PatronRequest{ID: "sam-id-1"}, nil)

	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))
	msg := iso18626.ISO18626Message{
		Request: &iso18626.Request{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "req-id-1",
				SupplyingAgencyRequestId:  "sam-id-1",
			},
		},
	}
	pr, err := handler.getPatronRequest(appCtx, msg)
	assert.NoError(t, err)
	assert.Equal(t, "req-id-1", pr.ID)

	msg = iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "req-id-1",
				SupplyingAgencyRequestId:  "sam-id-1",
			},
		},
	}
	pr, err = handler.getPatronRequest(appCtx, msg)
	assert.NoError(t, err)
	assert.Equal(t, "sam-id-1", pr.ID)

	msg = iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: "ISIL",
					},
					AgencyIdValue: "SUP1",
				},
				RequestingAgencyRequestId: "req-id-1",
				SupplyingAgencyRequestId:  "",
			},
		},
	}
	pr, err = handler.getPatronRequest(appCtx, msg)
	assert.NoError(t, err)
	assert.Equal(t, "sam-id-1", pr.ID)

	msg = iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyRequestId: "req-id-1",
				SupplyingAgencyRequestId:  "sam-id-1",
			},
		},
	}
	pr, err = handler.getPatronRequest(appCtx, msg)
	assert.NoError(t, err)
	assert.Equal(t, "req-id-1", pr.ID)

	msg = iso18626.ISO18626Message{}
	pr, err = handler.getPatronRequest(appCtx, msg)
	assert.Equal(t, "missing message", err.Error())
	assert.Equal(t, "", pr.ID)
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

	status, resp, err := handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusError, status)
	assert.Nil(t, resp)
	assert.Equal(t, "cannot process message without content", err.Error())

	status, resp, err = handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{Request: &iso18626.Request{}}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "missing RequestingAgencyRequestId", resp.RequestConfirmation.ErrorData.ErrorValue)
	assert.Nil(t, err)

	status, resp, err = handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{}}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "unknown action: ", resp.RequestingAgencyMessageConfirmation.ErrorData.ErrorValue)
	assert.Equal(t, "unknown action: ", err.Error())

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("db error"))
	status, resp, err = handler.handlePatronRequestMessage(appCtx, &iso18626.ISO18626Message{SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{Header: iso18626.Header{RequestingAgencyRequestId: patronRequestId}}}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "status change no allowed", err.Error())
	assert.Equal(t, "status change no allowed", resp.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
}

func TestHandleSupplyingAgencyMessageExpectToSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusExpectToSupply},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NoError(t, err)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateSupplierLocated, mockPrRepo.savedPr.State)
}

func TestHandleSupplyingAgencyMessageWillSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateWillSupply, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageWillSupplyCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply},
		MessageInfo: iso18626.MessageInfo{
			Note: RESHARE_ADD_LOAN_CONDITION + " some comment",
		},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateConditionPending, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageLoaned(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned},
		MessageInfo: iso18626.MessageInfo{
			Note: "#MultipleItems#\n1|2|3\n#MultipleItemsEnd#",
		},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateShipped, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
	assert.Len(t, mockPrRepo.savedItems, 1)
}

func TestHandleSupplyingAgencyMessageLoanCompleted(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusLoanCompleted},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCompleted, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageUnfilled(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusUnfilled},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateUnfilled, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageCancelled(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCancelled, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleSupplyingAgencyMessageNoImplemented(t *testing.T) {
	handler := CreatePatronRequestMessageHandler(new(MockPrRepo), *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusEmpty},
	}, pr_db.PatronRequest{})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "status change no allowed", resp.SupplyingAgencyMessageConfirmation.ErrorData.ErrorValue)
	assert.Equal(t, "status change no allowed", err.Error())
}

type MockIllRepo struct {
	mock.Mock
	ill_db.PgIllRepo
}

func TestHandleSupplyingAgencyMessageCancelledFailToSave(t *testing.T) {
	handler := CreatePatronRequestMessageHandler(new(MockPrRepo), *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleSupplyingAgencyMessage(appCtx, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		StatusInfo: iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled},
	}, pr_db.PatronRequest{ID: "error"})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "db error", err.Error())
}

func TestHandleRequestingAgencyMessageNotification(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		Action: iso18626.TypeActionNotification,
	}, pr_db.PatronRequest{State: LenderStateWillSupply})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleRequestingAgencyMessageCancel(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		Action: iso18626.TypeActionCancel,
	}, pr_db.PatronRequest{State: LenderStateWillSupply})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, LenderStateCancelRequested, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleRequestingAgencyMessageShippedReturn(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		Action: iso18626.TypeActionShippedReturn,
	}, pr_db.PatronRequest{State: LenderStateWillSupply})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, LenderStateShippedReturn, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleRequestingAgencyMessageUnknown(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		Action: "unknown",
	}, pr_db.PatronRequest{State: LenderStateWillSupply})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "unknown action: unknown", err.Error())
}

func TestHandleRequestingAgencyMessageFailToSave(t *testing.T) {
	handler := CreatePatronRequestMessageHandler(new(MockPrRepo), *new(events.EventRepo), *new(ill_db.IllRepo), *new(events.EventBus))

	status, resp, err := handler.handleRequestingAgencyMessage(appCtx, iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyRequestId: patronRequestId,
		},
		Action: iso18626.TypeActionShippedReturn,
	}, pr_db.PatronRequest{State: LenderStateWillSupply, ID: "error"})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "db error", err.Error())
}

func TestHandleRequestMessage(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockPrRepo.On("GetPatronRequestBySupplierSymbolAndRequesterReqId", "ISIL:SUP1", "req-id-1").Return(pr_db.PatronRequest{}, pgx.ErrNoRows)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	status, resp, err := handler.handleRequestMessage(appCtx, iso18626.Request{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "REQ1",
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: "req-id-1",
		},
	})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resp.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, LenderStateNew, mockPrRepo.savedPr.State)
	assert.NoError(t, err)
}

func TestHandleRequestMessageMissingRequestId(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockPrRepo.On("GetPatronRequestBySupplierSymbolAndRequesterReqId", "ISIL:SUP1", "req-id-1").Return(pr_db.PatronRequest{}, pgx.ErrNoRows)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	status, resp, err := handler.handleRequestMessage(appCtx, iso18626.Request{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "REQ1",
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: "",
		},
	})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "missing RequestingAgencyRequestId", resp.RequestConfirmation.ErrorData.ErrorValue)
	assert.NoError(t, err)
}

func TestHandleRequestMessageExistingRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockPrRepo.On("GetPatronRequestBySupplierSymbolAndRequesterReqId", "ISIL:SUP1", "req-id-1").Return(pr_db.PatronRequest{}, nil)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	status, resp, err := handler.handleRequestMessage(appCtx, iso18626.Request{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "REQ1",
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: "req-id-1",
		},
	})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "there is already request with this id req-id-1", resp.RequestConfirmation.ErrorData.ErrorValue)
	assert.Equal(t, "duplicate request: there is already a request with this id req-id-1", err.Error())
}

func TestHandleRequestMessageSearchDbError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockPrRepo.On("GetPatronRequestBySupplierSymbolAndRequesterReqId", "ISIL:SUP1", "req-id-1").Return(pr_db.PatronRequest{}, errors.New("db error"))
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	status, resp, err := handler.handleRequestMessage(appCtx, iso18626.Request{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "REQ1",
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: "req-id-1",
		},
	})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "db error", resp.RequestConfirmation.ErrorData.ErrorValue)
	assert.Equal(t, "db error", err.Error())
}

func TestHandleRequestMessageSaveError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockPrRepo.On("GetPatronRequestBySupplierSymbolAndRequesterReqId", "ISIL:SUP1", "error").Return(pr_db.PatronRequest{}, pgx.ErrNoRows)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	status, resp, err := handler.handleRequestMessage(appCtx, iso18626.Request{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "REQ1",
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: "ISIL",
				},
				AgencyIdValue: "SUP1",
			},
			RequestingAgencyRequestId: "error",
		},
	})
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resp.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, "db error", resp.RequestConfirmation.ErrorData.ErrorValue)
	assert.Equal(t, "db error", err.Error())
}

func TestSaveItems(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), mockEventBus)

	// Empty message
	sam := iso18626.SupplyingAgencyMessage{}
	err := handler.saveItems(appCtx, pr_db.PatronRequest{ID: "pr1"}, sam)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(mockPrRepo.savedItems))

	// One Item
	sam.MessageInfo.Note = "#MultipleItems#\n1|2|3\n#MultipleItemsEnd#"
	err = handler.saveItems(appCtx, pr_db.PatronRequest{ID: "pr1"}, sam)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(mockPrRepo.savedItems))
	assert.Equal(t, "1", mockPrRepo.savedItems[0].Title.String)
	assert.Equal(t, "2", mockPrRepo.savedItems[0].CallNumber.String)
	assert.Equal(t, "3", mockPrRepo.savedItems[0].ItemID.String)
	assert.Equal(t, "3", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "pr1", mockPrRepo.savedItems[0].PrID)

	// Two Items
	sam.MessageInfo.Note = "#MultipleItems#\n1|2|3\n4,5|6|7\n#MultipleItemsEnd#"
	mockPrRepo.savedItems = nil
	err = handler.saveItems(appCtx, pr_db.PatronRequest{ID: "pr1"}, sam)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(mockPrRepo.savedItems))
	assert.Equal(t, "1", mockPrRepo.savedItems[0].Title.String)
	assert.Equal(t, "2", mockPrRepo.savedItems[0].CallNumber.String)
	assert.Equal(t, "3", mockPrRepo.savedItems[0].ItemID.String)
	assert.Equal(t, "3", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "pr1", mockPrRepo.savedItems[0].PrID)
	assert.Equal(t, "4,5", mockPrRepo.savedItems[1].Title.String)
	assert.Equal(t, "6", mockPrRepo.savedItems[1].CallNumber.String)
	assert.Equal(t, "7", mockPrRepo.savedItems[1].ItemID.String)
	assert.Equal(t, "7", mockPrRepo.savedItems[1].Barcode)
	assert.Equal(t, "pr1", mockPrRepo.savedItems[1].PrID)
}

func TestExtractRamNotifications(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), new(MockEventBus))
	// No note
	err := handler.extractRamNotifications(appCtx, pr_db.PatronRequest{ID: "1"}, iso18626.RequestingAgencyMessage{})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(mockPrRepo.savedNotifications))

	// Note
	err = handler.extractRamNotifications(appCtx, pr_db.PatronRequest{ID: "1"}, iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP",
			},
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "REQ",
			},
		},
		Note: "save this",
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))
	assert.Equal(t, "save this", mockPrRepo.savedNotifications[0].Note.String)
	assert.Equal(t, "ISIL:REQ", mockPrRepo.savedNotifications[0].FromSymbol)
	assert.Equal(t, "ISIL:SUP", mockPrRepo.savedNotifications[0].ToSymbol)

	// Error
	mockPrRepo.savedNotifications = nil
	err = handler.extractRamNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.RequestingAgencyMessage{
		Note: "save this",
	})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))
}

func TestExtractRequestNotifications(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), new(MockEventBus))
	// No note
	err := handler.extractRequestNotifications(appCtx, pr_db.PatronRequest{ID: "1"}, iso18626.Request{})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(mockPrRepo.savedNotifications))

	// Note
	err = handler.extractRequestNotifications(appCtx, pr_db.PatronRequest{ID: "1"}, iso18626.Request{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP",
			},
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "REQ",
			},
		},
		ServiceInfo: &iso18626.ServiceInfo{Note: "save this"},
		BillingInfo: &iso18626.BillingInfo{MaximumCosts: &iso18626.TypeCosts{
			CurrencyCode: iso18626.TypeSchemeValuePair{Text: "EUR"},
			MonetaryValue: utils.XSDDecimal{
				Base: 123,
				Exp:  -2,
			},
		}},
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(mockPrRepo.savedNotifications))
	assert.Equal(t, "save this", mockPrRepo.savedNotifications[0].Note.String)
	assert.Equal(t, "ISIL:REQ", mockPrRepo.savedNotifications[0].FromSymbol)
	assert.Equal(t, "ISIL:SUP", mockPrRepo.savedNotifications[0].ToSymbol)
	cost, err := mockPrRepo.savedNotifications[1].Cost.Float64Value()
	assert.NoError(t, err)
	assert.Equal(t, 1.23, cost.Float64)

	// Error
	mockPrRepo.savedNotifications = nil
	err = handler.extractRequestNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{Note: "save this"}})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))

	// Error
	mockPrRepo.savedNotifications = nil
	err = handler.extractRequestNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.Request{
		BillingInfo: &iso18626.BillingInfo{MaximumCosts: &iso18626.TypeCosts{
			CurrencyCode: iso18626.TypeSchemeValuePair{Text: "EUR"},
			MonetaryValue: utils.XSDDecimal{
				Base: 123,
				Exp:  -2,
			},
		}},
	})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))
}
func TestExtractSamNotifications(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	handler := CreatePatronRequestMessageHandler(mockPrRepo, *new(events.EventRepo), *new(ill_db.IllRepo), new(MockEventBus))
	// No note
	err := handler.extractSamNotifications(appCtx, pr_db.PatronRequest{ID: "1"}, iso18626.SupplyingAgencyMessage{})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(mockPrRepo.savedNotifications))

	// Note
	err = handler.extractSamNotifications(appCtx, pr_db.PatronRequest{ID: "1"}, iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "SUP",
			},
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
				AgencyIdValue: "REQ",
			},
		},
		MessageInfo: iso18626.MessageInfo{
			Note: "save this",
			OfferedCosts: &iso18626.TypeCosts{
				CurrencyCode: iso18626.TypeSchemeValuePair{Text: "EUR"},
				MonetaryValue: utils.XSDDecimal{
					Base: 124,
					Exp:  -2,
				},
			},
		},
		DeliveryInfo: &iso18626.DeliveryInfo{
			DeliveryCosts: &iso18626.TypeCosts{
				CurrencyCode: iso18626.TypeSchemeValuePair{Text: "EUR"},
				MonetaryValue: utils.XSDDecimal{
					Base: 125,
					Exp:  -2,
				},
			},
			LoanCondition: &iso18626.TypeSchemeValuePair{
				Text: "library use only",
			},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, 4, len(mockPrRepo.savedNotifications))
	assert.Equal(t, "save this", mockPrRepo.savedNotifications[0].Note.String)
	assert.Equal(t, "ISIL:SUP", mockPrRepo.savedNotifications[0].FromSymbol)
	assert.Equal(t, "ISIL:REQ", mockPrRepo.savedNotifications[0].ToSymbol)
	cost, err := mockPrRepo.savedNotifications[1].Cost.Float64Value()
	assert.NoError(t, err)
	assert.Equal(t, 1.24, cost.Float64)
	cost, err = mockPrRepo.savedNotifications[2].Cost.Float64Value()
	assert.NoError(t, err)
	assert.Equal(t, 1.25, cost.Float64)
	assert.Equal(t, "library use only", mockPrRepo.savedNotifications[3].Condition.String)

	// Error
	mockPrRepo.savedNotifications = nil
	err = handler.extractSamNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{Note: "save this"},
	})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))

	mockPrRepo.savedNotifications = nil
	err = handler.extractSamNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.SupplyingAgencyMessage{
		MessageInfo: iso18626.MessageInfo{
			OfferedCosts: &iso18626.TypeCosts{
				CurrencyCode: iso18626.TypeSchemeValuePair{Text: "EUR"},
				MonetaryValue: utils.XSDDecimal{
					Base: 124,
					Exp:  -2,
				},
			},
		},
	})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))

	mockPrRepo.savedNotifications = nil
	err = handler.extractSamNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.SupplyingAgencyMessage{
		DeliveryInfo: &iso18626.DeliveryInfo{
			DeliveryCosts: &iso18626.TypeCosts{
				CurrencyCode: iso18626.TypeSchemeValuePair{Text: "EUR"},
				MonetaryValue: utils.XSDDecimal{
					Base: 125,
					Exp:  -2,
				},
			},
		},
	})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))

	mockPrRepo.savedNotifications = nil
	err = handler.extractSamNotifications(appCtx, pr_db.PatronRequest{ID: "error"}, iso18626.SupplyingAgencyMessage{
		DeliveryInfo: &iso18626.DeliveryInfo{
			LoanCondition: &iso18626.TypeSchemeValuePair{
				Text: "library use only",
			},
		},
	})
	assert.Equal(t, "db error", err.Error())
	assert.Equal(t, 1, len(mockPrRepo.savedNotifications))
}
