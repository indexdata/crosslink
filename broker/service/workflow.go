package service

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/client"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
)

type WorkflowManager struct {
	eventBus   events.EventBus
	illRepo    ill_db.IllRepo
	config     WorkflowConfig
	brokerMode client.BrokerMode
}

type WorkflowConfig struct {
}

func CreateWorkflowManager(eventBus events.EventBus, illRepo ill_db.IllRepo, config WorkflowConfig, brokerMode client.BrokerMode) WorkflowManager {
	return WorkflowManager{
		eventBus:   eventBus,
		illRepo:    illRepo,
		config:     config,
		brokerMode: brokerMode,
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
			if w.brokerMode == client.BrokerModeTransparent {
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
		ctx.Logger().Error("failed to process event because missing SupplyingAgencyMessage")
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
	selSup, err := w.illRepo.GetSelectedSupplierForIllTransaction(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Error("failed to read selected supplier", "error", err)
		return
	}
	if selSup.LastAction.String != ill_db.RequestAction {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameConfirmRequesterMsg, events.EventData{}, &event.ID)
		}, "")
	} else if event.EventStatus != events.EventStatusSuccess {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
		}, "")
	}
}

func (w *WorkflowManager) shouldForwardMessage(ctx extctx.ExtendedContext, event events.Event) bool {
	if w.brokerMode == client.BrokerModeTransparent {
		sup, err := w.illRepo.GetSelectedSupplierForIllTransaction(ctx, event.IllTransactionID)
		if err != nil {
			ctx.Logger().Error("failed to process supplier message received event", "error", err)
			return false
		}
		return !sup.LocalSupplier
	}
	return true
}
