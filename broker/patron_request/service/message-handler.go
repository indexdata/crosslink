package prservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var SUPPLIER_PATRON_PATTERN = utils.GetEnv("SUPPLIER_PATRON_PATTERN", "%v_user")

const COMP_MESSAGE = "pr_massage_handler"
const RESHARE_ADD_LOAN_CONDITION = "#ReShareAddLoanCondition#"

type PatronRequestMessageHandler struct {
	prRepo               pr_db.PrRepo
	eventRepo            events.EventRepo
	illRepo              ill_db.IllRepo
	eventBus             events.EventBus
	actionMappingService ActionMappingService
	autoActionRunner     AutoActionRunner
}

type AutoActionRunner interface {
	RunAutoActionsOnStateEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentEventID *string) error
}

func CreatePatronRequestMessageHandler(prRepo pr_db.PrRepo, eventRepo events.EventRepo, illRepo ill_db.IllRepo, eventBus events.EventBus) PatronRequestMessageHandler {
	return PatronRequestMessageHandler{
		prRepo:               prRepo,
		eventRepo:            eventRepo,
		illRepo:              illRepo,
		eventBus:             eventBus,
		actionMappingService: ActionMappingService{},
	}
}

func (m *PatronRequestMessageHandler) SetAutoActionRunner(autoActionRunner AutoActionRunner) {
	m.autoActionRunner = autoActionRunner
}

func (m *PatronRequestMessageHandler) runAutoActionsOnStateEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest) error {
	if m.autoActionRunner == nil {
		return nil
	}
	// Auto actions run inline so incoming-message confirmations can include their outcomes.
	return m.autoActionRunner.RunAutoActionsOnStateEntry(ctx, pr, nil)
}

func (m *PatronRequestMessageHandler) applyEventTransition(pr pr_db.PatronRequest, eventName string) (pr_db.PatronRequest, bool, bool) {
	transitionState, hasTransition, eventDefined := m.actionMappingService.GetActionMapping(pr).GetEventTransition(pr, eventName)
	if !eventDefined {
		return pr, false, false
	}
	if hasTransition && transitionState != pr.State {
		pr.State = transitionState
		return pr, true, true
	}
	return pr, false, true
}

func (m *PatronRequestMessageHandler) HandleMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message) (*iso18626.ISO18626Message, error) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP_MESSAGE))
	if msg == nil {
		return nil, errors.New("cannot process nil message")
	}

	pr, err := m.getPatronRequest(ctx, *msg)
	if err != nil {
		return nil, err
	}
	// Create notice with result
	status, response, err := m.handlePatronRequestMessage(ctx, msg, pr)
	eventData := events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: msg, OutgoingMessage: response}}
	if err != nil {
		eventData.EventError = &events.EventError{
			Message: err.Error(),
		}
	}
	_, err = m.eventBus.CreateNotice(pr.ID, events.EventNamePatronRequestMessage, eventData, status, events.EventDomainPatronRequest)
	if err != nil {
		return nil, err
	}

	return response, err
}

func (m *PatronRequestMessageHandler) handlePatronRequestMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	if msg.SupplyingAgencyMessage != nil {
		return m.handleSupplyingAgencyMessage(ctx, *msg.SupplyingAgencyMessage, pr)
	} else if msg.RequestingAgencyMessage != nil {
		return m.handleRequestingAgencyMessage(ctx, *msg.RequestingAgencyMessage, pr)
	} else if msg.Request != nil {
		return m.handleRequestMessage(ctx, *msg.Request)
	} else {
		return events.EventStatusError, nil, errors.New("cannot process message without content")
	}
}

func (m *PatronRequestMessageHandler) getPatronRequest(ctx common.ExtendedContext, msg iso18626.ISO18626Message) (pr_db.PatronRequest, error) {
	if msg.SupplyingAgencyMessage != nil {
		return m.prRepo.GetPatronRequestById(ctx, msg.SupplyingAgencyMessage.Header.RequestingAgencyRequestId)
	} else if msg.RequestingAgencyMessage != nil {
		if msg.RequestingAgencyMessage.Header.SupplyingAgencyRequestId != "" {
			return m.prRepo.GetPatronRequestById(ctx, msg.RequestingAgencyMessage.Header.SupplyingAgencyRequestId)
		} else {
			symbol := msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue
			return m.prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(ctx, symbol, msg.RequestingAgencyMessage.Header.RequestingAgencyRequestId)
		}
	} else if msg.Request != nil {
		return m.prRepo.GetPatronRequestById(ctx, msg.Request.Header.RequestingAgencyRequestId)
	} else {
		return pr_db.PatronRequest{}, errors.New("missing message")
	}
}

func (m *PatronRequestMessageHandler) handleSupplyingAgencyMessage(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventName := ""
	switch sam.StatusInfo.Status {
	case iso18626.TypeStatusExpectToSupply:
		eventName = "expect-to-supply"
		supSymbol := sam.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + sam.Header.SupplyingAgencyId.AgencyIdValue
		pr.SupplierSymbol = pgtype.Text{
			String: supSymbol,
			Valid:  true,
		}
	case iso18626.TypeStatusWillSupply:
		if strings.Contains(sam.MessageInfo.Note, RESHARE_ADD_LOAN_CONDITION) {
			eventName = "will-supply-conditional"
		} else {
			eventName = "will-supply"
		}
	case iso18626.TypeStatusLoaned:
		eventName = "loaned"
	case iso18626.TypeStatusLoanCompleted, iso18626.TypeStatusCopyCompleted:
		eventName = "completed"
	case iso18626.TypeStatusUnfilled:
		eventName = "unfilled"
	case iso18626.TypeStatusCancelled:
		eventName = "cancel-accepted"
	}

	if eventName == "" {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "status change not allowed",
		}, errors.New("status change not allowed"))
	}

	updatedPr, stateChanged, eventDefined := m.applyEventTransition(pr, eventName)
	if !eventDefined {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "status change not allowed",
		}, errors.New("status change not allowed"))
	}
	return m.updatePatronRequestAndCreateSamResponse(ctx, updatedPr, sam, stateChanged)
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateSamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage, stateChanged bool) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(pr))
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	if stateChanged {
		err = m.runAutoActionsOnStateEntry(ctx, pr)
		if err != nil {
			return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
	}
	return createSAMResponse(sam, iso18626.TypeMessageStatusOK, nil, nil)
}

func createSAMResponse(sam iso18626.SupplyingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &iso18626.ISO18626Message{
			SupplyingAgencyMessageConfirmation: &iso18626.SupplyingAgencyMessageConfirmation{
				ConfirmationHeader: iso18626.ConfirmationHeader{
					SupplyingAgencyId:         &sam.Header.SupplyingAgencyId,
					RequestingAgencyId:        &sam.Header.RequestingAgencyId,
					RequestingAgencyRequestId: sam.Header.RequestingAgencyRequestId,
					MessageStatus:             messageStatus,
				},
				ReasonForMessage: &sam.MessageInfo.ReasonForMessage,
				ErrorData:        errorData,
			},
		},
		err
}

func createRequestResponse(request iso18626.Request, messageStatus iso18626.TypeMessageStatus, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &iso18626.ISO18626Message{
			RequestConfirmation: &iso18626.RequestConfirmation{
				ConfirmationHeader: iso18626.ConfirmationHeader{
					SupplyingAgencyId:         &request.Header.SupplyingAgencyId,
					RequestingAgencyId:        &request.Header.RequestingAgencyId,
					RequestingAgencyRequestId: request.Header.RequestingAgencyRequestId,
					MessageStatus:             messageStatus,
				},
				ErrorData: errorData,
			},
		},
		err
}

func (m *PatronRequestMessageHandler) handleRequestMessage(ctx common.ExtendedContext, request iso18626.Request) (events.EventStatus, *iso18626.ISO18626Message, error) {
	raRequestId := request.Header.RequestingAgencyRequestId
	if raRequestId == "" {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "missing RequestingAgencyRequestId",
		}, nil)
	}
	supplierSymbol := request.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + request.Header.SupplyingAgencyId.AgencyIdValue
	requesterSymbol := request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + request.Header.RequestingAgencyId.AgencyIdValue
	_, err := m.prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(ctx, supplierSymbol, raRequestId)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
	} else {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeBadlyFormedMessage,
			ErrorValue: "there is already request with this id " + raRequestId,
		}, errors.New("duplicate request: there is already a request with this id "+raRequestId))
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	pr, err := m.prRepo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams{
		ID:              uuid.NewString(),
		Timestamp:       pgtype.Timestamp{Valid: true, Time: time.Now()},
		State:           LenderStateNew,
		Side:            SideLending,
		Patron:          getDbText(fmt.Sprintf(SUPPLIER_PATRON_PATTERN, request.Header.SupplyingAgencyId.AgencyIdValue)),
		RequesterSymbol: getDbText(requesterSymbol),
		IllRequest:      requestBytes,
		SupplierSymbol:  getDbText(supplierSymbol),
		RequesterReqID:  getDbText(raRequestId),
	})
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	err = m.runAutoActionsOnStateEntry(ctx, pr)
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}

	return createRequestResponse(request, iso18626.TypeMessageStatusOK, nil, nil)
}

func getDbText(value string) pgtype.Text {
	return pgtype.Text{
		Valid:  true,
		String: value,
	}
}

func (m *PatronRequestMessageHandler) handleRequestingAgencyMessage(ctx common.ExtendedContext, ram iso18626.RequestingAgencyMessage, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	switch ram.Action {
	case iso18626.TypeActionNotification,
		iso18626.TypeActionStatusRequest,
		iso18626.TypeActionRenew,
		iso18626.TypeActionShippedForward,
		iso18626.TypeActionReceived:
		return m.updatePatronRequestAndCreateRamResponse(ctx, pr, ram, &ram.Action, false)
	case iso18626.TypeActionCancel:
		updatedPr, stateChanged, eventDefined := m.applyEventTransition(pr, "cancel-request")
		if !eventDefined {
			err := errors.New("unsupported action: " + string(ram.Action))
			return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, &ram.Action, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnsupportedActionType,
				ErrorValue: err.Error(),
			}, err)
		}
		return m.updatePatronRequestAndCreateRamResponse(ctx, updatedPr, ram, &ram.Action, stateChanged)
	case iso18626.TypeActionShippedReturn:
		updatedPr, stateChanged, eventDefined := m.applyEventTransition(pr, "shipped-return")
		if !eventDefined {
			err := errors.New("unsupported action: " + string(ram.Action))
			return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, &ram.Action, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnsupportedActionType,
				ErrorValue: err.Error(),
			}, err)
		}
		return m.updatePatronRequestAndCreateRamResponse(ctx, updatedPr, ram, &ram.Action, stateChanged)
	}
	err := errors.New("unsupported action: " + string(ram.Action))
	return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, &ram.Action, &iso18626.ErrorData{
		ErrorType:  iso18626.TypeErrorTypeUnsupportedActionType,
		ErrorValue: err.Error(),
	}, err)
}

func createRAMResponse(ram iso18626.RequestingAgencyMessage, messageStatus iso18626.TypeMessageStatus, action *iso18626.TypeAction, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &iso18626.ISO18626Message{
			RequestingAgencyMessageConfirmation: &iso18626.RequestingAgencyMessageConfirmation{
				ConfirmationHeader: iso18626.ConfirmationHeader{
					SupplyingAgencyId:         &ram.Header.SupplyingAgencyId,
					RequestingAgencyId:        &ram.Header.RequestingAgencyId,
					RequestingAgencyRequestId: ram.Header.RequestingAgencyRequestId,
					MessageStatus:             messageStatus,
				},
				Action:    action,
				ErrorData: errorData,
			},
		},
		err
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateRamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, ram iso18626.RequestingAgencyMessage, action *iso18626.TypeAction, stateChanged bool) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(pr))
	if err != nil {
		return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, action, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	if stateChanged {
		err = m.runAutoActionsOnStateEntry(ctx, pr)
		if err != nil {
			return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, action, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
	}
	return createRAMResponse(ram, iso18626.TypeMessageStatusOK, action, nil, nil)
}
