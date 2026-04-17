package prservice

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/shim"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var SUPPLIER_PATRON_PATTERN = utils.GetEnv("SUPPLIER_PATRON_PATTERN", "%v_user")

const COMP_MESSAGE = "pr_massage_handler"

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
		actionMappingService: ActionMappingService{SMService: &StateModelService{}},
	}
}

func (m *PatronRequestMessageHandler) SetAutoActionRunner(autoActionRunner AutoActionRunner) {
	m.autoActionRunner = autoActionRunner
}

func (m *PatronRequestMessageHandler) runAutoActionsOnStateEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentEventID *string) error {
	if m.autoActionRunner == nil {
		return nil
	}
	// Auto actions run inline so incoming-message confirmations can include their outcomes.
	return m.autoActionRunner.RunAutoActionsOnStateEntry(ctx, pr, parentEventID)
}

func (m *PatronRequestMessageHandler) applyEventTransition(pr pr_db.PatronRequest, eventName MessageEvent) (pr_db.PatronRequest, bool, bool, error) {
	actionMapping, err := m.actionMappingService.GetActionMapping(pr)
	if err != nil {
		return pr, false, false, err
	}
	transitionState, hasTransition, eventDefined := actionMapping.GetEventTransition(pr, string(eventName))
	if !eventDefined {
		return pr, false, false, nil
	}
	if hasTransition && transitionState != pr.State {
		pr.State = transitionState
		if config, configOk := actionMapping.getStateConfig(pr); configOk && config.terminal {
			pr.TerminalState = true
		}
		return pr, true, true, nil
	}
	return pr, false, true, nil
}

func (m *PatronRequestMessageHandler) HandleMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message) (*iso18626.ISO18626Message, error) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP_MESSAGE))
	if msg == nil {
		return nil, errors.New("cannot process nil message")
	}

	_, response, _, handleErr := m.handlePatronRequestMessage(ctx, msg)
	return response, handleErr
}

func (m *PatronRequestMessageHandler) getPatronRequestForRequestingAgencyMessage(ctx common.ExtendedContext, ram *iso18626.RequestingAgencyMessage) (pr_db.PatronRequest, error) {
	if ram.Header.SupplyingAgencyRequestId != "" {
		return m.prRepo.GetPatronRequestByIdAndSide(ctx, ram.Header.SupplyingAgencyRequestId, SideLending)
	}
	symbol := ram.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + ram.Header.SupplyingAgencyId.AgencyIdValue
	return m.prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx, symbol, ram.Header.RequestingAgencyRequestId)
}

func (m *PatronRequestMessageHandler) processPatronRequestMessageTask(
	ctx common.ExtendedContext,
	prID string,
	incoming *iso18626.ISO18626Message,
	handler func(execCtx common.ExtendedContext, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error),
) (events.EventStatus, *iso18626.ISO18626Message, error) {
	data := events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: incoming}}
	eventID, err := m.eventBus.CreateTask(prID, events.EventNamePatronRequestMessage, data, events.EventDomainPatronRequest, nil, events.SignalConsumers)
	if err != nil {
		return events.EventStatusError, nil, err
	}

	var (
		handlerStatus events.EventStatus
		response      *iso18626.ISO18626Message
		handleErr     error
	)

	_, err = m.eventBus.ProcessTask(ctx, events.Event{
		ID:              eventID,
		EventName:       events.EventNamePatronRequestMessage,
		PatronRequestID: prID,
		EventData:       data,
	}, events.SignalConsumers, func(taskCtx common.ExtendedContext, task events.Event) (events.EventStatus, *events.EventResult) {
		handlerStatus, response, handleErr = handler(taskCtx, taskParentID(&task))
		result := &events.EventResult{
			CommonEventData: events.CommonEventData{
				IncomingMessage: incoming,
				OutgoingMessage: response,
			},
		}
		if handleErr != nil {
			result.EventError = &events.EventError{Message: handleErr.Error()}
		}
		return handlerStatus, result
	})
	if err != nil {
		return events.EventStatusError, nil, err
	}
	return handlerStatus, response, handleErr
}

func taskParentID(task *events.Event) *string {
	if task == nil {
		return nil
	}
	return &task.ID
}

func (m *PatronRequestMessageHandler) handlePatronRequestMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message) (events.EventStatus, *iso18626.ISO18626Message, pr_db.PatronRequest, error) {
	if msg.SupplyingAgencyMessage != nil {
		pr, err := m.prRepo.GetPatronRequestByIdAndSide(ctx, msg.SupplyingAgencyMessage.Header.RequestingAgencyRequestId, SideBorrowing)
		if err != nil {
			return events.EventStatusError, nil, pr_db.PatronRequest{}, err
		}
		status, response, err := m.processPatronRequestMessageTask(ctx, pr.ID, msg, func(execCtx common.ExtendedContext, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
			return m.handleSupplyingAgencyMessageWithParent(execCtx, *msg.SupplyingAgencyMessage, pr, parentEventID)
		})
		return status, response, pr, err
	} else if msg.RequestingAgencyMessage != nil {
		pr, err := m.getPatronRequestForRequestingAgencyMessage(ctx, msg.RequestingAgencyMessage)
		if err != nil {
			return events.EventStatusError, nil, pr_db.PatronRequest{}, err
		}
		status, response, err := m.processPatronRequestMessageTask(ctx, pr.ID, msg, func(execCtx common.ExtendedContext, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
			return m.handleRequestingAgencyMessageWithParent(execCtx, *msg.RequestingAgencyMessage, pr, parentEventID)
		})
		return status, response, pr, err
	} else if msg.Request != nil {
		status, response, pr, err := m.handleRequestMessage(ctx, *msg.Request)
		return status, response, pr, err
	} else {
		return events.EventStatusError, nil, pr_db.PatronRequest{}, errors.New("cannot process message without content")
	}
}

func (m *PatronRequestMessageHandler) handleSupplyingAgencyMessage(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	return m.handleSupplyingAgencyMessageWithParent(ctx, sam, pr, nil)
}

func (m *PatronRequestMessageHandler) handleSupplyingAgencyMessageWithParent(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage, pr pr_db.PatronRequest, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
	unsupportedReason := func() (events.EventStatus, *iso18626.ISO18626Message, error) {
		err := fmt.Errorf("unsupported reason for message: %s", sam.MessageInfo.ReasonForMessage)
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnsupportedReasonForMessageType,
			ErrorValue: err.Error(),
		}, err)
	}
	statusChangeNotAllowed := func() (events.EventStatus, *iso18626.ISO18626Message, error) {
		err := fmt.Errorf("status change not allowed: %s", sam.StatusInfo.Status)
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	contradictoryCancelResponse := func() (events.EventStatus, *iso18626.ISO18626Message, error) {
		err := fmt.Errorf("contradictory cancel response: status=%s answerYesNo=%v", sam.StatusInfo.Status, sam.MessageInfo.AnswerYesNo)
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}

	switch sam.MessageInfo.ReasonForMessage {
	case iso18626.TypeReasonForMessageNotification:
		// Notifications are acknowledged but must not drive state transitions.
		notErr := m.extractSamNotifications(ctx, pr, sam)
		if notErr != nil {
			ctx.Logger().Error("failed to save sam notifications", "error", notErr)
		}
		return createSAMResponse(sam, iso18626.TypeMessageStatusOK, nil, nil)
	case iso18626.TypeReasonForMessageStatusChange,
		iso18626.TypeReasonForMessageRequestResponse,
		iso18626.TypeReasonForMessageCancelResponse:
		// continue to status mapping
	default:
		return unsupportedReason()
	}

	supSymbol := sam.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + sam.Header.SupplyingAgencyId.AgencyIdValue
	if supSymbol != ":" {
		pr.SupplierSymbol = pgtype.Text{
			String: supSymbol,
			Valid:  true,
		}
	}

	eventName := MessageEvent("")
	switch sam.StatusInfo.Status {
	case iso18626.TypeStatusExpectToSupply:
		eventName = SupplierExpectToSupply
	case iso18626.TypeStatusWillSupply:
		if sam.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageCancelResponse {
			if sam.MessageInfo.AnswerYesNo != nil && *sam.MessageInfo.AnswerYesNo == iso18626.TypeYesNoY {
				return contradictoryCancelResponse()
			}
			eventName = SupplierCancelRejected
		} else if strings.Contains(sam.MessageInfo.Note, shim.RESHARE_ADD_LOAN_CONDITION) {
			eventName = SupplierWillSupplyCond
		} else {
			eventName = SupplierWillSupply
		}
	case iso18626.TypeStatusLoaned:
		err := m.saveItems(ctx, pr, sam)
		if err != nil {
			return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
		eventName = SupplierLoaned
	case iso18626.TypeStatusLoanCompleted, iso18626.TypeStatusCopyCompleted:
		eventName = SupplierCompleted
	case iso18626.TypeStatusUnfilled:
		eventName = SupplierUnfilled
	case iso18626.TypeStatusCancelled:
		// Cancellation transition is accepted only for cancel-response messages.
		if sam.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageCancelResponse {
			if sam.MessageInfo.AnswerYesNo != nil && *sam.MessageInfo.AnswerYesNo == iso18626.TypeYesNoN {
				return contradictoryCancelResponse()
			}
			eventName = SupplierCancelAccepted
		}
	}

	if eventName == "" {
		return statusChangeNotAllowed()
	}

	updatedPr, stateChanged, eventDefined, err := m.applyEventTransition(pr, eventName)
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	if !eventDefined {
		return statusChangeNotAllowed()
	}
	return m.updatePatronRequestAndCreateSamResponse(ctx, updatedPr, sam, stateChanged, parentEventID)
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateSamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage, stateChanged bool, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(pr))
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	err = m.extractSamNotifications(ctx, pr, sam)
	if err != nil {
		ctx.Logger().Error("failed to save sam notifications", "error", err)
	}
	if stateChanged {
		err = m.runAutoActionsOnStateEntry(ctx, pr, parentEventID)
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
	var message = iso18626.NewISO18626Message()
	message.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{
			SupplyingAgencyId:         &sam.Header.SupplyingAgencyId,
			RequestingAgencyId:        &sam.Header.RequestingAgencyId,
			RequestingAgencyRequestId: sam.Header.RequestingAgencyRequestId,
			TimestampReceived:         sam.Header.Timestamp,
			Timestamp:                 utils.XSDDateTime{Time: time.Now()},
			MessageStatus:             messageStatus,
		},
		ReasonForMessage: &sam.MessageInfo.ReasonForMessage,
		ErrorData:        errorData,
	}
	return eventStatus, message, err
}

func createRequestResponse(request iso18626.Request, messageStatus iso18626.TypeMessageStatus, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	var message = iso18626.NewISO18626Message()
	message.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{
			SupplyingAgencyId:         &request.Header.SupplyingAgencyId,
			RequestingAgencyId:        &request.Header.RequestingAgencyId,
			RequestingAgencyRequestId: request.Header.RequestingAgencyRequestId,
			TimestampReceived:         request.Header.Timestamp,
			Timestamp:                 utils.XSDDateTime{Time: time.Now()},
			MessageStatus:             messageStatus,
		},
		ErrorData: errorData,
	}
	return eventStatus, message, err
}

func (m *PatronRequestMessageHandler) handleRequestMessage(ctx common.ExtendedContext, request iso18626.Request) (events.EventStatus, *iso18626.ISO18626Message, pr_db.PatronRequest, error) {
	raRequestId := request.Header.RequestingAgencyRequestId
	if raRequestId == "" {
		status, response, err := createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "missing RequestingAgencyRequestId",
		}, nil)
		return status, response, pr_db.PatronRequest{}, err
	}
	supplierSymbol := request.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + request.Header.SupplyingAgencyId.AgencyIdValue
	requesterSymbol := request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + request.Header.RequestingAgencyId.AgencyIdValue
	existingPr, err := m.prRepo.GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx, supplierSymbol, raRequestId)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			status, response, handleErr := createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
			return status, response, pr_db.PatronRequest{}, handleErr
		}
	} else {
		var message = iso18626.NewISO18626Message()
		message.Request = &request
		status, response, handleErr := m.processPatronRequestMessageTask(ctx, existingPr.ID, message,
			func(_ common.ExtendedContext, _ *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
				return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
					ErrorType:  iso18626.TypeErrorTypeBadlyFormedMessage,
					ErrorValue: "there is already request with this id " + raRequestId,
				}, errors.New("duplicate request: there is already a request with this id "+raRequestId))
			})
		return status, response, existingPr, handleErr
	}
	pr, err := m.prRepo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams{
		ID:              uuid.NewString(),
		CreatedAt:       pgtype.Timestamp{Valid: true, Time: time.Now()},
		State:           LenderStateNew,
		Side:            SideLending,
		Patron:          getDbText(fmt.Sprintf(SUPPLIER_PATRON_PATTERN, request.Header.SupplyingAgencyId.AgencyIdValue)),
		RequesterSymbol: getDbText(requesterSymbol),
		IllRequest:      request,
		SupplierSymbol:  getDbText(supplierSymbol),
		RequesterReqID:  getDbText(raRequestId),
		Language:        pr_db.LANGUAGE,
		Items:           []pr_db.PrItem{},
		TerminalState:   false,
	})
	if err != nil {
		status, response, handleErr := createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
		return status, response, pr_db.PatronRequest{}, handleErr
	}
	err = m.extractRequestNotifications(ctx, pr, request)
	if err != nil {
		ctx.Logger().Error("failed to save request notifications", "error", err)
	}
	var message = iso18626.NewISO18626Message()
	message.Request = &request
	status, response, handleErr := m.processPatronRequestMessageTask(ctx, pr.ID, message,
		func(execCtx common.ExtendedContext, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
			err = m.runAutoActionsOnStateEntry(execCtx, pr, parentEventID)
			if err != nil {
				return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
					ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
					ErrorValue: err.Error(),
				}, err)
			}
			return createRequestResponse(request, iso18626.TypeMessageStatusOK, nil, nil)
		})
	if handleErr != nil {
		return status, response, pr_db.PatronRequest{}, handleErr
	}
	return status, response, pr, handleErr
}

func getDbText(value string) pgtype.Text {
	return pgtype.Text{
		Valid:  true,
		String: value,
	}
}

func getDbTextPtr(value *string) pgtype.Text {
	if value == nil || *value == "" {
		return pgtype.Text{
			Valid: false,
		}
	}
	return getDbText(*value)
}

func (m *PatronRequestMessageHandler) handleRequestingAgencyMessage(ctx common.ExtendedContext, ram iso18626.RequestingAgencyMessage, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	return m.handleRequestingAgencyMessageWithParent(ctx, ram, pr, nil)
}

func (m *PatronRequestMessageHandler) handleRequestingAgencyMessageWithParent(ctx common.ExtendedContext, ram iso18626.RequestingAgencyMessage, pr pr_db.PatronRequest, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
	unsupported := func() (events.EventStatus, *iso18626.ISO18626Message, error) {
		err := errors.New("unsupported action: " + string(ram.Action))
		return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, &ram.Action, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnsupportedActionType,
			ErrorValue: err.Error(),
		}, err)
	}

	eventName := MessageEvent("")
	switch ram.Action {
	case iso18626.TypeActionNotification:
		if strings.Contains(ram.Note, shim.RESHARE_LOAN_CONDITION_AGREE) {
			eventName = RequesterCondAccepted
		} else {
			// Notifications are acknowledged but must not drive state transitions.
			notErr := m.extractRamNotifications(ctx, pr, ram)
			if notErr != nil {
				ctx.Logger().Error("failed to save ram notifications", "error", notErr)
			}
			return createRAMResponse(ram, iso18626.TypeMessageStatusOK, &ram.Action, nil, nil)
		}
	case iso18626.TypeActionCancel:
		if strings.Contains(ram.Note, shim.RESHARE_LOAN_CONDITION_REJECT) {
			eventName = RequesterCondRejected
		} else {
			// TODO: ReShare currently does not send an explicit reject-condition marker on
			// cancel messages. Detect regular cancel vs condition rejection from the
			// incoming message shape once that distinction is known.
			eventName = RequesterCancelRequest
		}
	case iso18626.TypeActionReceived:
		eventName = RequesterReceived
	case iso18626.TypeActionShippedReturn:
		eventName = RequesterShippedReturn
	default:
		return unsupported()
	}

	updatedPr, stateChanged, eventDefined, err := m.applyEventTransition(pr, eventName)
	if err != nil {
		return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, &ram.Action, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	if !eventDefined {
		return unsupported()
	}
	return m.updatePatronRequestAndCreateRamResponse(ctx, updatedPr, ram, &ram.Action, stateChanged, parentEventID)
}

func createRAMResponse(ram iso18626.RequestingAgencyMessage, messageStatus iso18626.TypeMessageStatus, action *iso18626.TypeAction, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	var message = iso18626.NewISO18626Message()
	message.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{
			SupplyingAgencyId:         &ram.Header.SupplyingAgencyId,
			RequestingAgencyId:        &ram.Header.RequestingAgencyId,
			RequestingAgencyRequestId: ram.Header.RequestingAgencyRequestId,
			TimestampReceived:         ram.Header.Timestamp,
			Timestamp:                 utils.XSDDateTime{Time: time.Now()},
			MessageStatus:             messageStatus,
		},
		Action:    action,
		ErrorData: errorData,
	}
	return eventStatus, message, err
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateRamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, ram iso18626.RequestingAgencyMessage, action *iso18626.TypeAction, stateChanged bool, parentEventID *string) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(pr))
	if err != nil {
		return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, action, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	err = m.extractRamNotifications(ctx, pr, ram)
	if err != nil {
		ctx.Logger().Error("failed to save ram notifications", "error", err)
	}
	if stateChanged {
		err = m.runAutoActionsOnStateEntry(ctx, pr, parentEventID)
		if err != nil {
			return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, action, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
	}
	return createRAMResponse(ram, iso18626.TypeMessageStatusOK, action, nil, nil)
}

func (m *PatronRequestMessageHandler) saveItems(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) error {
	result, _, _ := common.UnpackItemsNote(sam.MessageInfo.Note)
	for _, item := range result {
		var loopErr error
		if len(item) == 1 && item[0] != "" {
			loopErr = m.saveItem(ctx, pr.ID, &item[0], nil, nil)
		} else if len(item) == 3 {
			loopErr = m.saveItem(ctx, pr.ID, &item[0], &item[1], &item[2])
		} else {
			loopErr = errors.New("incorrect item param count: " + strconv.Itoa(len(item)))
		}
		if loopErr != nil {
			return loopErr
		}
	}
	return nil
}

func (m *PatronRequestMessageHandler) saveItem(ctx common.ExtendedContext, prId string, supplierBarcode *string, callNumber *string, name *string) error {
	// not using supplier barcode as it may not be unique, using prId instead to link item to request, as
	// each request can have only one item without barcode and prId is unique for each request
	requesterBarcode := prId
	_, err := m.prRepo.SaveItem(ctx, pr_db.SaveItemParams{
		ID:         uuid.NewString(),
		CreatedAt:  pgtype.Timestamp{Valid: true, Time: time.Now()},
		PrID:       prId,
		ItemID:     getDbTextPtr(supplierBarcode),
		Title:      getDbTextPtr(name),
		CallNumber: getDbTextPtr(callNumber),
		Barcode:    requesterBarcode,
	})
	return err
}

func (m *PatronRequestMessageHandler) extractSamNotifications(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) error {
	supSymbol, reqSymbol := getSymbolsFromHeader(sam.Header)
	var note pgtype.Text
	noteText := stripReShareConditionMarkers(stripItemsNotePayload(sam.MessageInfo.Note))
	if noteText != "" {
		note = getDbText(noteText)
	}
	var condition pgtype.Text
	if sam.DeliveryInfo != nil && sam.DeliveryInfo.LoanCondition != nil {
		if sam.DeliveryInfo.LoanCondition.Text != "" {
			condition = getDbText(sam.DeliveryInfo.LoanCondition.Text)
		}
	}

	var currency pgtype.Text
	var cost pgtype.Numeric
	if sam.MessageInfo.OfferedCosts != nil {
		var err error
		cost, currency, err = toNotificationCost(sam.MessageInfo.OfferedCosts)
		if err != nil {
			return err
		}
	} else if sam.DeliveryInfo != nil && sam.DeliveryInfo.DeliveryCosts != nil {
		var err error
		cost, currency, err = toNotificationCost(sam.DeliveryInfo.DeliveryCosts)
		if err != nil {
			return err
		}
	}

	if !note.Valid && !condition.Valid && !cost.Valid {
		return nil
	}

	_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
		ID:         uuid.NewString(),
		PrID:       pr.ID,
		Note:       note,
		FromSymbol: supSymbol,
		ToSymbol:   reqSymbol,
		Direction:  pr_db.NotificationDirectionReceived,
		Kind:       inferNotificationKind(note.Valid, condition.Valid, cost.Valid),
		Condition:  condition,
		Currency:   currency,
		Cost:       cost,
		CreatedAt: pgtype.Timestamp{
			Valid: true,
			Time:  time.Now(),
		},
	})
	return err
}

func stripItemsNotePayload(note string) string {
	if note == "" {
		return ""
	}
	_, startIdx, endIdx := common.UnpackItemsNote(note)
	if startIdx < 0 || endIdx < 0 {
		return strings.TrimSpace(note)
	}
	before := strings.TrimSpace(note[:startIdx])
	afterStart := endIdx + len(common.MULTIPLE_ITEMS_END)
	after := ""
	if afterStart < len(note) {
		after = strings.TrimSpace(note[afterStart:])
	}
	switch {
	case before != "" && after != "":
		return before + "\n" + after
	case before != "":
		return before
	default:
		return after
	}
}

func (m *PatronRequestMessageHandler) extractRamNotifications(ctx common.ExtendedContext, pr pr_db.PatronRequest, ram iso18626.RequestingAgencyMessage) error {
	noteText := stripReShareConditionMarkers(ram.Note)
	if noteText == "" {
		return nil
	}
	supSymbol, reqSymbol := getSymbolsFromHeader(ram.Header)
	_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
		ID:         uuid.NewString(),
		PrID:       pr.ID,
		Note:       getDbText(noteText),
		FromSymbol: reqSymbol,
		ToSymbol:   supSymbol,
		Direction:  pr_db.NotificationDirectionReceived,
		Kind:       pr_db.NotificationKindNote,
		CreatedAt: pgtype.Timestamp{
			Valid: true,
			Time:  time.Now(),
		},
	})
	return err
}

func stripReShareConditionMarkers(note string) string {
	cleaned := note
	cleaned = strings.ReplaceAll(cleaned, shim.RESHARE_ADD_LOAN_CONDITION, "")
	cleaned = strings.ReplaceAll(cleaned, shim.RESHARE_LOAN_CONDITION_AGREE, "")
	cleaned = strings.ReplaceAll(cleaned, shim.RESHARE_LOAN_CONDITION_REJECT, "")
	return strings.TrimSpace(cleaned)
}

func toNotificationCost(value *iso18626.TypeCosts) (pgtype.Numeric, pgtype.Text, error) {
	costExp, err := safeConvertInt32(value.MonetaryValue.Exp)
	if err != nil {
		return pgtype.Numeric{}, pgtype.Text{}, err
	}
	return pgtype.Numeric{
			Valid: true,
			Int:   big.NewInt(int64(value.MonetaryValue.Base)),
			Exp:   costExp,
		},
		getDbText(value.CurrencyCode.Text),
		nil
}

func (m *PatronRequestMessageHandler) extractRequestNotifications(ctx common.ExtendedContext, pr pr_db.PatronRequest, request iso18626.Request) error {
	supSymbol, reqSymbol := getSymbolsFromHeader(request.Header)
	var note pgtype.Text
	if request.ServiceInfo != nil && request.ServiceInfo.Note != "" {
		note = getDbText(request.ServiceInfo.Note)
	}

	var currency pgtype.Text
	var cost pgtype.Numeric
	if request.BillingInfo != nil && request.BillingInfo.MaximumCosts != nil {
		var err error
		cost, currency, err = toNotificationCost(request.BillingInfo.MaximumCosts)
		if err != nil {
			return err
		}
	}

	if !note.Valid && !cost.Valid {
		return nil
	}

	_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
		ID:         uuid.NewString(),
		PrID:       pr.ID,
		Note:       note,
		FromSymbol: reqSymbol,
		ToSymbol:   supSymbol,
		Direction:  pr_db.NotificationDirectionReceived,
		Kind:       inferNotificationKind(note.Valid, false, cost.Valid),
		Currency:   currency,
		Cost:       cost,
		CreatedAt: pgtype.Timestamp{
			Valid: true,
			Time:  time.Now(),
		},
	})
	return err
}

func inferNotificationKind(hasNote bool, hasCondition bool, hasCost bool) pr_db.NotificationKind {
	if hasCondition || hasCost {
		return pr_db.NotificationKindCondition
	}
	return pr_db.NotificationKindNote
}

func getSymbolsFromHeader(header iso18626.Header) (string, string) {
	return header.SupplyingAgencyId.AgencyIdType.Text + ":" + header.SupplyingAgencyId.AgencyIdValue,
		header.RequestingAgencyId.AgencyIdType.Text + ":" + header.RequestingAgencyId.AgencyIdValue
}

func safeConvertInt32(n int) (int32, error) {
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("integer out of range for int32: %d", n)
	}
	return int32(n), nil
}
