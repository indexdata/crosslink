package service

import (
	"errors"
	"fmt"

	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
)

const WF_COMP = "workflow_manager"

var brokerSymbol = utils.GetEnv("BROKER_SYMBOL", "ISIL:BROKER")

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
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	extctx.Must(ctx, func() (string, error) {
		return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameLocateSuppliers, events.EventData{}, &event.ID)
	}, "")
}

func (w *WorkflowManager) OnLocateSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	extctx.Must(ctx, func() (string, error) {
		if event.EventStatus == events.EventStatusSuccess {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
		} else {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, &event.ID)
		}
	}, "")
}

func (w *WorkflowManager) OnSelectSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	extctx.Must(ctx, func() (string, error) {
		if event.EventStatus == events.EventStatusSuccess {
			requester, err := w.illRepo.GetRequesterByIllTransactionId(ctx, event.IllTransactionID)
			if err != nil {
				ctx.Logger().Error("failed to process supplier selected event, no requester", "error", err)
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
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	if event.EventStatus != events.EventStatusSuccess {
		return
	}
	if event.EventData.IncomingMessage == nil || event.EventData.IncomingMessage.SupplyingAgencyMessage == nil {
		ctx.Logger().Error("failed to process event because missing SupplyingAgencyMessage")
		return
	}
	if w.handleAndCheckCancelResponse(ctx, *event.EventData.IncomingMessage.SupplyingAgencyMessage, event.IllTransactionID) {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester,
				events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: event.EventData.IncomingMessage}, CustomData: map[string]any{extctx.DO_NOT_SEND: !w.shouldForwardMessage(ctx, event)}}, &event.ID)
		}, "")
	} else {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTaskBroadcast(event.IllTransactionID, events.EventNameConfirmSupplierMsg, events.EventData{}, &event.ID)
		}, "")
		extctx.Must(ctx, func() (string, error) { // This will also send unfilled message if no more suppliers
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
		}, "")
	}
}

func (w *WorkflowManager) RequesterMessageReceived(ctx extctx.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	if event.EventStatus == events.EventStatusSuccess {
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier,
				events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: event.EventData.IncomingMessage}, CustomData: map[string]any{extctx.DO_NOT_SEND: !w.shouldForwardMessage(ctx, event)}}, &event.ID)
		}, "")
	}
}

func (w *WorkflowManager) OnMessageSupplierComplete(ctx extctx.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	// there are three cases when we message supplier:
	// 1. new supplier was selected and we send a request message
	// 2. requester has sent a retry request and we forward it to the supplier
	// 3. requester has sent an action message to the supplier
	// only in case 3 we suspended the HTTP request in the handler and must resume it here
	if event.EventData.IncomingMessage != nil && event.EventData.IncomingMessage.RequestingAgencyMessage != nil {
		// action message was send by requester so we must relay the confirmation
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

func (w *WorkflowManager) handleAndCheckCancelResponse(ctx extctx.ExtendedContext, sam iso18626.SupplyingAgencyMessage, illTransId string) bool {
	if !cancelSuccessful(sam) {
		return true
	}
	lastEvent, err := w.eventBus.GetLatestRequestEventByAction(ctx, illTransId, string(iso18626.TypeActionCancel))
	if err != nil {
		ctx.Logger().Error("failed to to find last event with action cancel", "error", err)
		return true
	}
	if lastEvent.EventData.IncomingMessage == nil || lastEvent.EventData.IncomingMessage.RequestingAgencyMessage == nil {
		ctx.Logger().Error("last cancel event is missing requesting agency message", "error", err)
		return true
	}
	if getSupplierSymbol(*lastEvent.EventData.IncomingMessage.RequestingAgencyMessage) == brokerSymbol {
		// Requester does not know supplier or broker symbol was used, fully terminate transaction
		w.skipAllSuppliersByStatus(ctx, illTransId, ill_db.SupplierStateNewPg)
		return true
	}
	return false
}

func cancelSuccessful(sam iso18626.SupplyingAgencyMessage) bool {
	if sam.MessageInfo.ReasonForMessage != iso18626.TypeReasonForMessageCancelResponse {
		return false
	}
	return sam.StatusInfo.Status == iso18626.TypeStatusCancelled || (sam.MessageInfo.AnswerYesNo != nil && *sam.MessageInfo.AnswerYesNo == iso18626.TypeYesNoY)
}

func (w *WorkflowManager) OnMessageRequesterComplete(ctx extctx.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	if event.EventData.IncomingMessage != nil && event.EventData.IncomingMessage.SupplyingAgencyMessage != nil {
		// action message was send by supplier so we must relay the confirmation
		extctx.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTaskBroadcast(event.IllTransactionID, events.EventNameConfirmSupplierMsg, events.EventData{}, &event.ID)
		}, "")
		if event.EventData.IncomingMessage.SupplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusUnfilled {
			extctx.Must(ctx, func() (string, error) {
				return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, &event.ID)
			}, "")
		}
		if cancelSuccessful(*event.EventData.IncomingMessage.SupplyingAgencyMessage) {
			// If successful cancel is forwarded to requester then skip currently selected supplier
			w.skipAllSuppliersByStatus(ctx, event.IllTransactionID, ill_db.SupplierStateSelectedPg)
		}
	}
}

// TODO move this to the client, we're doing double work here
func (w *WorkflowManager) shouldForwardMessage(ctx extctx.ExtendedContext, event events.Event) bool {
	requester, err := w.illRepo.GetRequesterByIllTransactionId(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Info("cannot detect local supply: no requester", "error", err)
		return false
	}
	if requester.BrokerMode == string(extctx.BrokerModeTransparent) || requester.BrokerMode == string(extctx.BrokerModeTranslucent) {
		sup, err := w.illRepo.GetSelectedSupplierForIllTransaction(ctx, event.IllTransactionID)
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, pgx.ErrTooManyRows) {
			symbol := getSymbol(event.EventData.IncomingMessage)
			sup, err = w.illRepo.GetLocatedSupplierByIllTransactionAndSymbol(ctx, event.IllTransactionID, symbol)
			if err != nil || sup.SupplierStatus != ill_db.SupplierStateSkippedPg {
				ctx.Logger().Info("cannot detect local supply: no skipped supplier", "error", err)
				return false
			}
			return !sup.LocalSupplier
		}
		if err != nil {
			ctx.Logger().Info("cannot detect local supply: no selected supplier", "error", err)
			return false
		}
		return !sup.LocalSupplier
	}
	return true
}

func (w *WorkflowManager) skipAllSuppliersByStatus(ctx extctx.ExtendedContext, illTransId string, supplierStatus pgtype.Text) {
	suppliers, err := w.illRepo.GetLocatedSuppliersByIllTransactionAndStatus(ctx, ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams{
		IllTransactionID: illTransId,
		SupplierStatus:   supplierStatus,
	})
	if err != nil {
		ctx.Logger().Error("could not read supplier", "error", err)
		return
	}
	if len(suppliers) > 0 {
		for _, supplier := range suppliers {
			supplier.SupplierStatus = ill_db.SupplierStateSkippedPg
			_, err = w.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(supplier))
			if err != nil {
				ctx.Logger().Error("could not update selected supplier status", "error", err)
				return
			}
		}
	}
}

func getSymbol(msg *iso18626.ISO18626Message) string {
	symbol := ""
	if msg != nil && msg.SupplyingAgencyMessage != nil {
		symbol = msg.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text + ":" +
			msg.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue
	}
	return symbol
}

func getSupplierSymbol(ram iso18626.RequestingAgencyMessage) string {
	return ram.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + ram.Header.SupplyingAgencyId.AgencyIdValue
}
