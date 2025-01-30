package service

import (
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/iso18626"
)

type WorkflowManager struct {
	eventBus events.EventBus
}

func CreateWorkflowManager(eventBus events.EventBus) WorkflowManager {
	return WorkflowManager{
		eventBus: eventBus,
	}
}

func (w *WorkflowManager) RequestReceived(ctx extctx.ExtendedContext, event events.Event) {
	Must(ctx, w.eventBus.CreateTask(event.IllTransactionID, events.EventNameLocateSuppliers, events.EventData{}))
}

func (w *WorkflowManager) OnLocateSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventStatus == events.EventStatusSuccess {
		Must(ctx, w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}))
	} //TODO message-requester ?
}

func (w *WorkflowManager) OnSelectSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventStatus == events.EventStatusSuccess {
		Must(ctx, w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier, events.EventData{}))
	} //TODO message-requester ?
}

func (w *WorkflowManager) SupplierMessageReceived(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventData.ISO18626Message == nil || event.EventData.ISO18626Message.SupplyingAgencyMessage == nil {
		ctx.Logger().Error("failed to process event because missing SupplyingAgencyMessage")
		return
	}
	status := event.EventData.ISO18626Message.SupplyingAgencyMessage.StatusInfo.Status
	switch status {
	case iso18626.TypeStatusUnfilled:
		Must(ctx, w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}))
	case iso18626.TypeStatusLoaned,
		iso18626.TypeStatusOverdue,
		iso18626.TypeStatusRecalled,
		iso18626.TypeStatusCopyCompleted,
		iso18626.TypeStatusLoanCompleted,
		iso18626.TypeStatusCompletedWithoutReturn:
		Must(ctx, w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}))
	case iso18626.TypeStatusCancelled:
		Must(ctx, w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{})) // TODO Check message. Maybe need to send message to requester
	}
}

func Must(ctx extctx.ExtendedContext, err error) {
	if err != nil {
		ctx.Logger().Error(err.Error())
	}
}
