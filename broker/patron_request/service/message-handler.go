package prservice

import (
	"errors"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
	"sync"
)

const COMP_MESSAGE = "pr_massage_handler"
const RESHARE_ADD_LOAN_CONDITION = "#ReShareAddLoanCondition#"

var waitings = map[string]*sync.WaitGroup{}

type PatronRequestMessageHandler struct {
	prRepo    pr_db.PrRepo
	eventRepo events.EventRepo
	illRep    ill_db.IllRepo
	eventBus  events.EventBus
}

func CreatePatronRequestMessageHandler(prRepo pr_db.PrRepo, eventRepo events.EventRepo, illRep ill_db.IllRepo, eventBus events.EventBus) PatronRequestMessageHandler {
	return PatronRequestMessageHandler{
		prRepo:    prRepo,
		eventRepo: eventRepo,
		illRep:    illRep,
		eventBus:  eventBus,
	}
}

func (m *PatronRequestMessageHandler) HandleMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message) (*iso18626.ISO18626Message, error) {
	if msg == nil {
		return nil, errors.New("cannot process nil message")
	}

	requestId := getPatronRequestId(*msg)
	pr, err := m.prRepo.GetPatronRequestById(ctx, requestId)
	if err != nil {
		return nil, err
	}

	eventId, err := m.eventBus.CreateTask(pr.ID, events.EventNamePatronRequestMessage, events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: msg}}, events.EventClassPatronRequest, nil)
	if err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	waitings[eventId] = &wg
	wg.Wait()

	event, err := m.eventRepo.GetEvent(ctx, eventId)
	if err != nil {
		return nil, err
	}
	return event.ResultData.OutgoingMessage, nil
}

func (m *PatronRequestMessageHandler) PatronRequestMessage(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP_MESSAGE))
	_, _ = m.eventBus.ProcessTask(ctx, event, m.handlePatronRequestMessage)
	if waiting, ok := waitings[event.ID]; ok {
		waiting.Done()
	}
}

func (m *PatronRequestMessageHandler) handlePatronRequestMessage(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	if event.EventData.IncomingMessage.SupplyingAgencyMessage != nil {
		return m.handleSupplyingAgencyMessage(ctx, *event.EventData.IncomingMessage.SupplyingAgencyMessage)
	} else if event.EventData.IncomingMessage.RequestingAgencyMessage != nil {
		return events.EventStatusError, &events.EventResult{CommonEventData: events.CommonEventData{Note: "requesting agency message handling is not implemented yet"}}
	} else if event.EventData.IncomingMessage.Request != nil {
		return events.EventStatusError, &events.EventResult{CommonEventData: events.CommonEventData{Note: "request handling is not implemented yet"}}
	} else {
		return events.EventStatusError, &events.EventResult{CommonEventData: events.CommonEventData{Note: "cannot process message without content"}}
	}
}

func getPatronRequestId(msg iso18626.ISO18626Message) string {
	if msg.SupplyingAgencyMessage != nil {
		return msg.SupplyingAgencyMessage.Header.RequestingAgencyRequestId
	} else if msg.RequestingAgencyMessage != nil {
		return msg.RequestingAgencyMessage.Header.SupplyingAgencyRequestId
	} else if msg.Request != nil {
		return msg.Request.Header.RequestingAgencyRequestId
	} else {
		return ""
	}
}

func (m *PatronRequestMessageHandler) handleSupplyingAgencyMessage(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage) (events.EventStatus, *events.EventResult) {
	pr, err := m.prRepo.GetPatronRequestById(ctx, sam.Header.RequestingAgencyRequestId)
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "could not find patron request",
		})
	}
	// TODO handle notifications
	switch sam.StatusInfo.Status {
	case iso18626.TypeStatusExpectToSupply:
		pr.State = BorrowerStateSupplierLocated
		supSymbol := sam.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + sam.Header.SupplyingAgencyId.AgencyIdValue
		supplier, err := m.illRep.GetPeerBySymbol(ctx, supSymbol)
		if err != nil {
			return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: "could not find supplier",
			})
		}
		pr.LendingPeerID = pgtype.Text{
			String: supplier.ID,
			Valid:  true,
		}
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusWillSupply:
		if strings.Contains(sam.MessageInfo.Note, RESHARE_ADD_LOAN_CONDITION) {
			pr.State = BorrowerStateConditionPending
			// TODO Save conditions
		} else {
			pr.State = BorrowerStateWillSupply
		}
		// TODO should we check if supplier is set ? and search if not
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusLoaned:
		pr.State = BorrowerStateShipped
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusLoanCompleted, iso18626.TypeStatusCopyCompleted:
		pr.State = BorrowerStateCompleted
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusUnfilled:
		pr.State = BorrowerStateUnfilled
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusCancelled:
		pr.State = BorrowerStateCancelled
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	}
	return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
		ErrorType:  iso18626.TypeErrorTypeBadlyFormedMessage,
		ErrorValue: "status change no allowed",
	})
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateSamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) (events.EventStatus, *events.EventResult) {
	_, err := m.prRepo.SavePatronRequest(ctx, pr_db.SavePatronRequestParams(pr))
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		})
	}
	return createSAMResponse(sam, iso18626.TypeMessageStatusOK, nil)
}

func createSAMResponse(sam iso18626.SupplyingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorData *iso18626.ErrorData) (events.EventStatus, *events.EventResult) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &events.EventResult{CommonEventData: events.CommonEventData{
		OutgoingMessage: &iso18626.ISO18626Message{
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
	}}
}
