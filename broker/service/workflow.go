package service

import (
	"fmt"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
)

const _COMP = "workflow_manager"

type WorkflowManager struct {
	eventBus events.EventBus
	illRepo  ill_db.IllRepo
	config   WorkflowConfig
}

type WorkflowConfig struct {
}

func CreateWorkflowManager(eventBus events.EventBus, illRepo ill_db.IllRepo, config WorkflowConfig) WorkflowManager {
	return WorkflowManager{
		eventBus: eventBus,
		illRepo:  illRepo,
		config:   config,
	}
}

func (w *WorkflowManager) RequestReceived(ctx extctx.ExtendedContext, event events.Event) {
	extctx.Must(ctx, func() (string, error) {
		return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameLocateSuppliers, events.EventData{}, &event.ID)
	}, "")
}

func (w *WorkflowManager) OnLocateSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	extctx.Must(ctx, func() (string, error) {
		if event.EventStatus == events.EventStatusSuccess {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
		} else {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID)
		}
	}, "")
}

func (w *WorkflowManager) OnSelectSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	extctx.Must(ctx, func() (string, error) {
		if event.EventStatus == events.EventStatusSuccess {
			requester, err := w.illRepo.GetRequesterByIllTransactionId(ctx, event.IllTransactionID)
			if err != nil {
				ctx.Logger().Error("failed to process supplier selected event, no requester", "error", err, "component", _COMP)
				return "", fmt.Errorf("failed to process supplier selected event, no requester")
			}
			if requester.BrokerMode == string(extctx.BrokerModeTransparent) || requester.BrokerMode == string(extctx.BrokerModeTranslucent) {
				id, err := w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID)
				if err != nil {
					return id, err
				}
			}
			if local, ok := event.ResultData.CustomData["localSupplier"].(bool); ok {
				if !local {
					return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier, events.EventData{}, &event.ID)
				} else {
					return "", nil
				}
			} else {
				return "", fmt.Errorf("failed to detect local supplier from event result data")
			}
		} else {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID)
		}
	}, "")
}

func (w *WorkflowManager) SupplierMessageReceived(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventStatus != events.EventStatusSuccess {
		return
	}
	if event.EventData.IncomingMessage == nil || event.EventData.IncomingMessage.SupplyingAgencyMessage == nil {
		ctx.Logger().Error("failed to process event because missing SupplyingAgencyMessage", "component", _COMP)
		return
	}
	status := event.EventData.IncomingMessage.SupplyingAgencyMessage.StatusInfo.Status
	switch status {
	case iso18626.TypeStatusUnfilled:
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
		}, "")
	default:
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester,
				events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: event.EventData.IncomingMessage}, CustomData: map[string]any{"doNotSend": !w.shouldForwardMessage(ctx, event)}}, &event.ID)
		}, "")
	}
}

func (w *WorkflowManager) RequesterMessageReceived(ctx extctx.ExtendedContext, event events.Event) {
	if event.EventStatus == events.EventStatusSuccess {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier,
				events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: event.EventData.IncomingMessage}, CustomData: map[string]any{"doNotSend": !w.shouldForwardMessage(ctx, event)}}, &event.ID)
		}, "")
	}
}

func (w *WorkflowManager) OnMessageSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	illTrans, err := w.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read ILL transaction", "error", err, "component", _COMP)
		return
	}
	if illTrans.LastRequesterAction.String != ill_db.RequestAction {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTaskBroadcast(event.IllTransactionID, events.EventNameConfirmRequesterMsg, events.EventData{}, &event.ID)
		}, "")
	} else if event.EventStatus != events.EventStatusSuccess {
		// if the last requester action was Request and messaging supplier failed, we try next supplier
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
		}, "")
	}
}

func (w *WorkflowManager) shouldForwardMessage(ctx extctx.ExtendedContext, event events.Event) bool {
	requester, err := w.illRepo.GetRequesterByIllTransactionId(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to process ISO18626 message received event, no requester", "error", err, "component", _COMP)
		return false
	}
	if requester.BrokerMode == string(extctx.BrokerModeTransparent) || requester.BrokerMode == string(extctx.BrokerModeTranslucent) {
		sup, err := w.illRepo.GetSelectedSupplierForIllTransaction(ctx, event.IllTransactionID)
		if err != nil {
			ctx.Logger().Error("failed to process ISO18626 message received event, no supplier", "error", err, "component", _COMP)
			return false
		}
		return !sup.LocalSupplier
	}
	return true
}
