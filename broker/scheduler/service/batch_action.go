package sched_service

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
)

const BATCH_COMP = "batch_action"

type BatchActionService struct {
	eventBus           events.EventBus
	emailSenderService *EmailSenderService
}

func NewBatchActionService(eventBus events.EventBus, emailSenderService *EmailSenderService) *BatchActionService {
	return &BatchActionService{
		eventBus:           eventBus,
		emailSenderService: emailSenderService,
	}
}
func (s *BatchActionService) BatchAction(ctx common.ExtendedContext, event events.Event) {
	_, _ = s.eventBus.ProcessTask(ctx, event, events.SignalConsumers, s.batchAction)
}
func (s *BatchActionService) batchAction(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(BATCH_COMP))
	if event.EventData.BatchActionData != nil {
		switch event.EventData.BatchActionData.ActionName {
		case string(schedoapi.BatchActionActionNameEmailPullslips):
			return s.emailSenderService.EmailPullslip(ctx, event)
		default:
			ctx.Logger().Error("unknown batch action", "actionName", event.EventData.BatchActionData.ActionName, "event", event)
			return events.NewErrorResult("cannot process event", "unknown batch action")
		}
	}
	ctx.Logger().Error("batch action data is empty", "event", event)
	return events.NewErrorResult("cannot process event", "batch action data is empty")
}
