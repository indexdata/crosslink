package prservice

import (
	"errors"
	"strings"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

const COMP_MESSAGE = "pr_massage_handler"
const RESHARE_ADD_LOAN_CONDITION = "#ReShareAddLoanCondition#"

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
	// Create notice with result
	status, response, err := m.handlePatronRequestMessage(ctx, msg)
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

func (m *PatronRequestMessageHandler) handlePatronRequestMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message) (events.EventStatus, *iso18626.ISO18626Message, error) {
	if msg.SupplyingAgencyMessage != nil {
		return m.handleSupplyingAgencyMessage(ctx, *msg.SupplyingAgencyMessage)
	} else if msg.RequestingAgencyMessage != nil {
		return events.EventStatusError, nil, errors.New("requesting agency message handling is not implemented yet")
	} else if msg.Request != nil {
		return events.EventStatusError, nil, errors.New("request handling is not implemented yet")
	} else {
		return events.EventStatusError, nil, errors.New("cannot process message without content")
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

func (m *PatronRequestMessageHandler) handleSupplyingAgencyMessage(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage) (events.EventStatus, *iso18626.ISO18626Message, error) {
	pr, err := m.prRepo.GetPatronRequestById(ctx, sam.Header.RequestingAgencyRequestId)
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "could not find patron request: " + err.Error(),
		}, err)
	}
	// TODO handle notifications
	switch sam.StatusInfo.Status {
	case iso18626.TypeStatusExpectToSupply:
		pr.State = BorrowerStateSupplierLocated
		supSymbol := sam.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + sam.Header.SupplyingAgencyId.AgencyIdValue
		pr.SupplierSymbol = pgtype.Text{
			String: supSymbol,
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
	}, errors.New("status change no allowed"))
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateSamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.SavePatronRequest(ctx, pr_db.SavePatronRequestParams(pr))
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
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
