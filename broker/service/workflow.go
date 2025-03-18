package service

import (
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
)

type WorkflowManager struct {
	eventBus events.EventBus
	illRepo  ill_db.IllRepo
}

func CreateWorkflowManager(eventBus events.EventBus, illRepo ill_db.IllRepo) WorkflowManager {
	return WorkflowManager{
		eventBus: eventBus,
		illRepo:  illRepo,
	}
}

func (w *WorkflowManager) RequestReceived(ctx extctx.ExtendedContext, event events.Event) {
	extctx.Must(ctx, func() (string, error) {
		return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameLocateSuppliers, events.EventData{}, &event.ID)
	})
}

func (w *WorkflowManager) OnLocateSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventStatus == events.EventStatusSuccess {
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID))
	} else {
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID))
	}
}

func (w *WorkflowManager) OnSelectSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventStatus == events.EventStatusSuccess {
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier, events.EventData{}, &event.ID))
	} else {
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID))
	}
}

func (w *WorkflowManager) SupplierMessageReceived(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventData.ISO18626Message == nil || event.EventData.ISO18626Message.SupplyingAgencyMessage == nil {
		ctx.Logger().Error("failed to process event because missing SupplyingAgencyMessage")
		return
	}
	status := event.EventData.ISO18626Message.SupplyingAgencyMessage.StatusInfo.Status
	switch status {
	case iso18626.TypeStatusUnfilled:
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID))
	case iso18626.TypeStatusLoaned,
		iso18626.TypeStatusOverdue,
		iso18626.TypeStatusRecalled,
		iso18626.TypeStatusCopyCompleted,
		iso18626.TypeStatusLoanCompleted,
		iso18626.TypeStatusCompletedWithoutReturn:
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID))
	case iso18626.TypeStatusCancelled:
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)) // TODO Check message. Maybe need to send message to requester
	}
}

func (w *WorkflowManager) RequesterMessageReceived(ctx extctx.ExtendedContext, event events.Event) {
	ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier, events.EventData{}, &event.ID))
}

func (w *WorkflowManager) OnMessageSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	selSup, err := w.illRepo.GetSelectedSupplierForIllTransaction(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read selected supplier", "error", err)
		return
	}
	if selSup.LastAction.String != ill_db.RequestAction {
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameConfirmRequesterMsg, events.EventData{}, &event.ID))
	} else if event.EventStatus != events.EventStatusSuccess {
		ctx.Must(w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID))
	}
}
