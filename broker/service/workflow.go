package service

import (
	"errors"
	"fmt"

	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/indexdata/crosslink/broker/common"
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

func (w *WorkflowManager) RequestReceived(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	common.Must(ctx, func() (string, error) {
		return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameLocateSuppliers, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
	}, "")
}

func (w *WorkflowManager) OnLocateSupplierComplete(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	common.Must(ctx, func() (string, error) {
		if event.EventStatus == events.EventStatusSuccess {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		} else {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}
	}, "")
}

func (w *WorkflowManager) OnCheckAvailabilityComplete(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	common.Must(ctx, func() (string, error) {
		if event.EventStatus != events.EventStatusSuccess {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}
		skipped, ok := event.ResultData.CustomData["skipped"].(bool)
		if !ok {
			return "", fmt.Errorf("failed to detect if supplier is skipped by availability check")
		}
		if skipped {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}
		id, err := w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		if err != nil {
			return id, err
		}
		local, ok := event.ResultData.CustomData["localSupplier"].(bool)
		if !ok {
			return "", fmt.Errorf("failed to detect local supplier from event result data")
		}
		if local {
			return "", nil
		}
		return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
	}, "")
}

func (w *WorkflowManager) OnSelectSupplierComplete(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	common.Must(ctx, func() (string, error) {
		if event.EventStatus != events.EventStatusSuccess {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}
		return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameCheckAvailability, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
	}, "")
}

func (w *WorkflowManager) SupplierMessageReceived(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	if event.EventStatus != events.EventStatusSuccess {
		return
	}
	if event.EventData.IncomingMessage == nil || event.EventData.IncomingMessage.SupplyingAgencyMessage == nil {
		ctx.Logger().Error("failed to process event because missing SupplyingAgencyMessage")
		return
	}
	if w.shouldForwardSAM(ctx, *event.EventData.IncomingMessage.SupplyingAgencyMessage, event.IllTransactionID) {
		common.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageRequester,
				events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: event.EventData.IncomingMessage}, CustomData: map[string]any{common.DO_NOT_SEND: !w.shouldForwardMessage(ctx, event)}}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}, "")
	} else {
		common.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameConfirmSupplierMsg, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalObservers)
		}, "")
		common.Must(ctx, func() (string, error) { // This will also send unfilled message if no more suppliers
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}, "")
	}
}

func (w *WorkflowManager) RequesterMessageReceived(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	if event.EventStatus == events.EventStatusSuccess {
		// If requester cancels via broker symbol, terminate remaining suppliers
		// regardless if cancellation is successful or not
		if terminalCancel(event) {
			w.skipAllSuppliersByStatus(ctx, event.IllTransactionID, ill_db.SupplierStateNewPg)
		}
		common.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameMessageSupplier,
				events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: event.EventData.IncomingMessage}, CustomData: map[string]any{common.DO_NOT_SEND: !w.shouldForwardMessage(ctx, event)}}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}, "")
	}
}

func terminalCancel(event events.Event) bool {
	if event.EventData.IncomingMessage == nil || event.EventData.IncomingMessage.RequestingAgencyMessage == nil {
		return false
	}
	ram := event.EventData.IncomingMessage.RequestingAgencyMessage
	return ram.Action == iso18626.TypeActionCancel && getSupplierSymbol(*ram) == brokerSymbol
}

func (w *WorkflowManager) OnMessageSupplierComplete(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	// there are three cases when we message supplier:
	// 1. new supplier was selected and we send a request message
	// 2. requester has sent a retry request and we forward it to the supplier
	// 3. requester has sent an action message to the supplier
	// only in case 3 we suspended the HTTP request in the handler and must resume it here
	if event.EventData.IncomingMessage != nil && event.EventData.IncomingMessage.RequestingAgencyMessage != nil {
		// action message was send by requester so we must relay the confirmation
		common.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameConfirmRequesterMsg, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalObservers)
		}, "")
	} else if event.EventStatus != events.EventStatusSuccess {
		// if the last requester action was Request and messaging supplier failed, we try next supplier
		common.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
		}, "")
	}
}

func (w *WorkflowManager) shouldForwardSAM(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage, illTransId string) bool {
	if !supplierAcceptedCancel(sam) {
		// always forward non-cancel response and rejected cancellation
		return true
	}
	cancelTarget, ok := w.latestRequesterCancelTarget(ctx, illTransId)
	if !ok {
		return true
	}
	if cancelTarget == brokerSymbol {
		// if requester used broker symbol to cancel we already terminated, so forward the response
		return true
	}
	// Suppress only the supplier's direct response to the requester cancel
	// unrelated cancel responses may be forwarded to the requester.
	return agencySymbol(sam.Header.SupplyingAgencyId) != cancelTarget
}

func (w *WorkflowManager) latestRequesterCancelTarget(ctx common.ExtendedContext, illTransId string) (string, bool) {
	lastEvent, err := w.eventBus.GetLatestRequestEventByAction(ctx, illTransId, string(iso18626.TypeActionCancel))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			ctx.Logger().Warn("latest requester cancel event not found", "error", err)
		} else {
			ctx.Logger().Error("failed to find latest requester cancel event", "error", err)
		}
		return "", false
	}
	if lastEvent.EventData.IncomingMessage == nil || lastEvent.EventData.IncomingMessage.RequestingAgencyMessage == nil {
		ctx.Logger().Error("last cancel event is missing requesting agency message")
		return "", false
	}
	return getSupplierSymbol(*lastEvent.EventData.IncomingMessage.RequestingAgencyMessage), true
}

func supplierAcceptedCancel(sam iso18626.SupplyingAgencyMessage) bool {
	if sam.MessageInfo.ReasonForMessage != iso18626.TypeReasonForMessageCancelResponse {
		return false
	}
	return sam.StatusInfo.Status == iso18626.TypeStatusCancelled || (sam.MessageInfo.AnswerYesNo != nil && *sam.MessageInfo.AnswerYesNo == iso18626.TypeYesNoY)
}

func supplierUnsolicitedCancel(sam iso18626.SupplyingAgencyMessage) bool {
	return sam.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageStatusChange &&
		sam.StatusInfo.Status == iso18626.TypeStatusCancelled
}

func (w *WorkflowManager) supplierAcceptedTerminalCancel(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage, illTransId string) bool {
	if !supplierAcceptedCancel(sam) {
		return false
	}
	cancelTarget, ok := w.latestRequesterCancelTarget(ctx, illTransId)
	return ok && cancelTarget == brokerSymbol
}

func (w *WorkflowManager) OnMessageRequesterComplete(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(WF_COMP))
	if event.EventData.IncomingMessage != nil && event.EventData.IncomingMessage.SupplyingAgencyMessage != nil {
		sam := *event.EventData.IncomingMessage.SupplyingAgencyMessage
		// action message was send by supplier so we must relay the confirmation
		common.Must(ctx, func() (string, error) {
			return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameConfirmSupplierMsg, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalObservers)
		}, "")
		if sam.StatusInfo.Status == iso18626.TypeStatusUnfilled {
			common.Must(ctx, func() (string, error) {
				return w.eventBus.CreateTask(event.IllTransactionID, events.EventNameSelectSupplier, events.EventData{}, events.EventDomainIllTransaction, &event.ID, events.SignalConsumers)
			}, "")
		}
		if w.supplierAcceptedTerminalCancel(ctx, sam, event.IllTransactionID) {
			// If requester sent terminal cancel we already skipped new suppliers but we also need to skip currently selected supplier
			w.skipAllSuppliersByStatus(ctx, event.IllTransactionID, ill_db.SupplierStateSelectedPg)
		}
		if supplierUnsolicitedCancel(sam) {
			w.skipAllSuppliersByStatus(ctx, event.IllTransactionID, ill_db.SupplierStateSelectedPg)
			w.skipAllSuppliersByStatus(ctx, event.IllTransactionID, ill_db.SupplierStateNewPg)
		}
	}
}

// TODO move this to the client, we're doing double work here
func (w *WorkflowManager) shouldForwardMessage(ctx common.ExtendedContext, event events.Event) bool {
	requester, err := w.illRepo.GetRequesterByIllTransactionId(ctx, event.IllTransactionID)
	if err != nil {
		ctx.Logger().Info("cannot detect local supply: no requester", "error", err)
		return false
	}
	if requester.BrokerMode == string(common.BrokerModeTransparent) {
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

func (w *WorkflowManager) skipAllSuppliersByStatus(ctx common.ExtendedContext, illTransId string, supplierStatus pgtype.Text) {
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
		symbol = agencySymbol(msg.SupplyingAgencyMessage.Header.SupplyingAgencyId)
	}
	return symbol
}

func getSupplierSymbol(ram iso18626.RequestingAgencyMessage) string {
	return agencySymbol(ram.Header.SupplyingAgencyId)
}

func agencySymbol(agencyId iso18626.TypeAgencyId) string {
	return agencyId.AgencyIdType.Text + ":" + agencyId.AgencyIdValue
}
