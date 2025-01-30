package service

import (
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
)

type SupplierLocator struct {
	eventBus        events.EventBus
	illRepo         ill_db.IllRepo
	dirAdapter      adapter.DirectoryLookupAdapter
	holdingsAdapter adapter.HoldingsLookupAdapter
}

func CreateSupplierLocator(eventBus events.EventBus, illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter, holdingsAdapter adapter.HoldingsLookupAdapter) SupplierLocator {
	return SupplierLocator{
		eventBus:        eventBus,
		illRepo:         illRepo,
		dirAdapter:      dirAdapter,
		holdingsAdapter: holdingsAdapter,
	}
}

func (s *SupplierLocator) LocateSuppliers(ctx extctx.ExtendedContext, event events.Event) {
	s.processTask(ctx, event, s.locateSuppliers)
}

func (s *SupplierLocator) SelectSupplier(ctx extctx.ExtendedContext, event events.Event) {
	s.processTask(ctx, event, s.selectSupplier)
}

func (s *SupplierLocator) processTask(ctx extctx.ExtendedContext, event events.Event, h func(extctx.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) {
	err := s.eventBus.BeginTask(event.ID)
	if err != nil {
		ctx.Logger().Error("failed to start event processing", "error", err)
		return
	}

	status, result := h(ctx, event)

	err = s.eventBus.CompleteTask(event.ID, result, status)
	if err != nil {
		ctx.Logger().Error("failed to complete event processing", "error", err)
	}
}

func (s *SupplierLocator) locateSuppliers(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := s.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read ill transaction", err)
	}

	if illTrans.SupplierRequestID.String == "" {
		return logProblemAndReturnResult(ctx, "ill transaction missing supplier request id")
	}

	holdings, err := s.holdingsAdapter.Lookup(adapter.HoldingLookupParams{
		Identifier: illTrans.SupplierRequestID.String,
	})

	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to locate holdings", err)
	}

	if len(holdings) == 0 {
		return logProblemAndReturnResult(ctx, "could not find holdings for supplier request id: "+illTrans.SupplierRequestID.String)
	}

	symbols := make([]string, 0, len(holdings))
	for _, holding := range holdings {
		symbols = append(symbols, holding.Symbol)
	}

	directories, err := s.dirAdapter.Lookup(adapter.DirectoryLookupParams{
		Symbols: symbols,
	})

	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to lookup directories: "+strings.Join(symbols, ","), err)
	}

	if len(directories) == 0 {
		return logProblemAndReturnResult(ctx, "could not find directories: "+strings.Join(symbols, ","))
	}

	count := int32(0)
	var locatedSuppliers []*ill_db.LocatedSupplier
	for _, dir := range directories {
		sup, err := s.addLocatedSupplier(ctx, dir, illTrans.ID, count)
		if err == nil {
			count++
			locatedSuppliers = append(locatedSuppliers, sup)
		}
	}

	if count == 0 {
		return logProblemAndReturnResult(ctx, "failed to add any supplier from: "+strings.Join(symbols, ","))
	}

	return events.EventStatusSuccess, getEventResult(map[string]any{"suppliers": locatedSuppliers})
}

func (s *SupplierLocator) addLocatedSupplier(ctx extctx.ExtendedContext, dir adapter.DirectoryEntry, transId string, ordinal int32) (*ill_db.LocatedSupplier, error) {
	peer, err := s.illRepo.GetPeerBySymbol(ctx, dir.Symbol)
	if err != nil {
		if err.Error() == "no rows in result set" {
			peer, err = s.illRepo.CreatePeer(ctx, ill_db.CreatePeerParams{
				ID:     uuid.New().String(),
				Symbol: dir.Symbol,
				Address: pgtype.Text{
					String: dir.URL,
					Valid:  true,
				},
				Name: dir.Symbol,
			})
		}
		if err != nil {
			ctx.Logger().Error("could not get peer by symbol", "symbol", dir.Symbol, "error", err)
			return nil, err
		}
	}

	supplier, err := s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: transId,
		SupplierID:       peer.ID,
		Ordinal:          ordinal,
		SupplierStatus: pgtype.Text{
			String: "new",
			Valid:  true,
		},
	})
	return &supplier, err
}

func (s *SupplierLocator) selectSupplier(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	suppliers, err := s.illRepo.GetLocatedSupplierByIllTransactionAndStatus(ctx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: event.IllTransactionID,
		SupplierStatus: pgtype.Text{
			String: "selected",
			Valid:  true,
		},
	})
	if err != nil {
		return logErrorAndReturnResult(ctx, "could not find selected suppliers", err)
	}
	if len(suppliers) > 0 {
		for _, supplier := range suppliers {
			supplier.SupplierStatus = pgtype.Text{
				String: "skipped",
				Valid:  true,
			}
			_, err = s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(supplier))
			if err != nil {
				return logErrorAndReturnResult(ctx, "could not update previous selected supplier", err)
			}
		}
	}
	suppliers, err = s.illRepo.GetLocatedSupplierByIllTransactionAndStatus(ctx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: event.IllTransactionID,
		SupplierStatus: pgtype.Text{
			String: "new",
			Valid:  true,
		},
	})
	if err != nil {
		return logErrorAndReturnResult(ctx, "could not find located suppliers", err)
	}
	if len(suppliers) == 0 {
		return logProblemAndReturnResult(ctx, "no suppliers with new status")
	}
	locSup := suppliers[0]
	locSup.SupplierStatus = pgtype.Text{
		String: "selected",
		Valid:  true,
	}
	locSup, err = s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locSup))
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to update located supplier status", err)
	}
	return events.EventStatusSuccess, getEventResult(map[string]any{"supplierId": locSup.SupplierID})
}

func logErrorAndReturnResult(ctx extctx.ExtendedContext, message string, err error) (events.EventStatus, *events.EventResult) {
	ctx.Logger().Error(message, "error", err)
	return events.EventStatusError, getEventResult(map[string]any{"message": message, "error": err})
}

func logProblemAndReturnResult(ctx extctx.ExtendedContext, message string) (events.EventStatus, *events.EventResult) {
	ctx.Logger().Info(message)
	return events.EventStatusProblem, getEventResult(map[string]any{"message": message})
}

func getEventResult(resultData map[string]any) *events.EventResult {
	return &events.EventResult{
		Data: resultData,
	}
}
