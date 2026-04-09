package prservice

import (
	"errors"
	"net/http"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
)

type PatronRequestNotificationService struct {
	PatronRequestMessageSender
	prRepo   pr_db.PrRepo
	eventBus events.EventBus
}

func CreatePatronRequestNotificationService(prRepo pr_db.PrRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface) *PatronRequestActionService {
	return &PatronRequestActionService{
		PatronRequestMessageSender: PatronRequestMessageSender{iso18626Handler: iso18626Handler},
		prRepo:                     prRepo,
		eventBus:                   eventBus,
	}
}

func (n *PatronRequestNotificationService) SendPatronRequestNotification(ctx common.ExtendedContext, pr pr_db.PatronRequest, notification pr_db.Notification) error {
	data := events.EventData{CustomData: map[string]any{"notification": notification}}
	eventID, err := n.eventBus.CreateTask(pr.ID, events.EventNameSendNotification, data, events.EventDomainPatronRequest, nil)
	if err != nil {
		return errors.New("failed to create event for patron request notification(" + notification.ID + "): " + err.Error())
	}

	notificationEvent := events.Event{
		ID:              eventID,
		PatronRequestID: pr.ID,
		EventData:       data,
	}

	_, err = n.processInvokeNotificationTask(ctx, notificationEvent)
	if err != nil {
		return errors.New("failed to process event for patron request notification(" + notification.ID + "): " + err.Error())
	}
	return nil
}

func (n *PatronRequestNotificationService) processInvokeNotificationTask(ctx common.ExtendedContext, event events.Event) (events.Event, error) {
	return n.eventBus.ProcessTask(ctx, event, n.handleInvokeNotification)
}

func (n *PatronRequestNotificationService) handleInvokeNotification(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	notification, ok := event.EventData.CustomData["notification"].(pr_db.Notification)
	if !ok {
		return logErrorAndReturnResult(ctx, "invalid event data: missing notification", nil)
	}
	pr, err := n.prRepo.GetPatronRequestById(ctx, event.PatronRequestID)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read patron request", err)
	}
	notification, err = n.prRepo.GetNotificationById(ctx, notification.ID)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read patron request", err)
	}
	result := events.EventResult{}
	if pr.Side == SideLending {
		note := "Send note"
		status, eventResult, httpStatus := n.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			Note:             note,
		}, iso18626.StatusInfo{}, nil)
		if httpStatus == nil {
			result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
			return events.EventStatusProblem, &result
		}
		if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
			result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
			result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
			return events.EventStatusProblem, &result
		}
		return events.EventStatusSuccess, &result
	} else {
		note := "Send note"
		n.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionNotification, note)
	}

}
