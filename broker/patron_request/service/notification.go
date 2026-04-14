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

func CreatePatronRequestNotificationService(prRepo pr_db.PrRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface) *PatronRequestNotificationService {
	return &PatronRequestNotificationService{
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
	notMap, ok := event.EventData.CustomData["notification"].(map[string]any)
	if !ok {
		return logErrorAndReturnResult(ctx, "invalid event data: missing notification", nil)
	}
	notificationId, ok := notMap["ID"].(string)
	if !ok {
		return logErrorAndReturnResult(ctx, "invalid event data: missing id", nil)
	}
	pr, err := n.prRepo.GetPatronRequestById(ctx, event.PatronRequestID)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read patron request", err)
	}
	notification, err := n.prRepo.GetNotificationById(ctx, notificationId)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read notification", err)
	}
	result := events.EventResult{}
	var status events.EventStatus
	var eventResult *events.EventResult
	var httpStatus *int
	var failure bool
	if pr.Side == SideLending {
		status, eventResult, httpStatus = n.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			Note:             notification.Note.String,
		}, iso18626.StatusInfo{}, nil)
		failure = result.IncomingMessage == nil || result.IncomingMessage.SupplyingAgencyMessageConfirmation == nil ||
			result.IncomingMessage.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK
	} else {
		status, eventResult, httpStatus = n.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionNotification, notification.Note.String)
		failure = result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
			result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK
	}

	if httpStatus == nil {
		notification.Receipt = pr_db.NotificationFailedToSent
		return n.updateNotification(ctx, notification, status, eventResult)
	}
	if *httpStatus != http.StatusOK || failure {
		result.EventError = &events.EventError{
			Message: "failed to send notification",
			Cause:   "not successful response",
		}
		notification.Receipt = pr_db.NotificationFailedToSent
		return n.updateNotification(ctx, notification, events.EventStatusProblem, &result)
	}
	notification.Receipt = pr_db.NotificationSent
	return n.updateNotification(ctx, notification, events.EventStatusSuccess, &result)
}

func (n *PatronRequestNotificationService) updateNotification(ctx common.ExtendedContext, notification pr_db.Notification, status events.EventStatus, result *events.EventResult) (events.EventStatus, *events.EventResult) {
	_, err := n.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams(notification))
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to update notification", err)
	}
	return status, result
}
