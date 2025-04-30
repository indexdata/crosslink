package service

import (
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
)

type SupplierLocator struct {
	eventBus        events.EventBus
	illRepo         ill_db.IllRepo
	dirAdapter      adapter.DirectoryLookupAdapter
	holdingsAdapter adapter.HoldingsLookupAdapter
	localSupply     bool
}

func CreateSupplierLocator(eventBus events.EventBus, illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter, holdingsAdapter adapter.HoldingsLookupAdapter, localSupply bool) SupplierLocator {
	return SupplierLocator{
		eventBus:        eventBus,
		illRepo:         illRepo,
		dirAdapter:      dirAdapter,
		holdingsAdapter: holdingsAdapter,
		localSupply:     localSupply,
	}
}

func (s *SupplierLocator) LocateSuppliers(ctx extctx.ExtendedContext, event events.Event) {
	s.eventBus.ProcessTask(ctx, event, s.locateSuppliers)
}

func (s *SupplierLocator) SelectSupplier(ctx extctx.ExtendedContext, event events.Event) {
	s.eventBus.ProcessTask(ctx, event, s.selectSupplier)
}

func (s *SupplierLocator) locateSuppliers(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := s.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read ILL transaction", err)
	}

	if illTrans.IllTransactionData.BibliographicInfo.SupplierUniqueRecordId == "" {
		return logProblemAndReturnResult(ctx, "ILL transaction missing SupplierUniqueRecordId")
	}

	requester, err := s.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to read requester peer", err)
	}

	holdings, err := s.holdingsAdapter.Lookup(adapter.HoldingLookupParams{
		Identifier: illTrans.IllTransactionData.BibliographicInfo.SupplierUniqueRecordId,
	})

	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to locate holdings", err)
	}

	if len(holdings) == 0 {
		return logProblemAndReturnResult(ctx, "could not find holdings for supplier request id: "+illTrans.IllTransactionData.BibliographicInfo.SupplierUniqueRecordId)
	}

	symbols := make([]string, 0, len(holdings))
	symLocalIdMapping := make(map[string]string, len(holdings))
	suppliersToAdd := make([]adapter.Supplier, 0, len(holdings))
	locallyAvailable := false
	var directory = map[string]any{}
	for _, holding := range holdings {
		symbols = append(symbols, holding.Symbol)
		symLocalIdMapping[holding.Symbol] = holding.LocalIdentifier
		if s.localSupply && illTrans.RequesterSymbol.Valid && holding.Symbol == illTrans.RequesterSymbol.String {
			locallyAvailable = true
		}
	}
	if locallyAvailable {
		if localId, ok := symLocalIdMapping[illTrans.RequesterSymbol.String]; ok {
			suppliersToAdd = append(suppliersToAdd, adapter.Supplier{
				PeerId:          requester.ID,
				CustomData:      requester.CustomData,
				LocalIdentifier: localId,
				Ratio:           1,
				Symbol:          illTrans.RequesterSymbol.String,
				Selected:        true,
			})
		}
	} else {
		peers, query := s.illRepo.GetCachedPeersBySymbols(ctx, symbols, s.dirAdapter)
		for _, peer := range peers {
			symList, err := s.illRepo.GetSymbolsByPeerId(ctx, peer.ID)
			if err != nil {
				return logErrorAndReturnResult(ctx, "failed to read symbols", err)
			}
			for _, sym := range symList {
				if localId, ok := symLocalIdMapping[sym.SymbolValue]; ok {
					suppliersToAdd = append(suppliersToAdd, adapter.Supplier{
						PeerId:          peer.ID,
						CustomData:      peer.CustomData,
						LocalIdentifier: localId,
						Ratio:           getPeerRatio(peer),
						Symbol:          sym.SymbolValue,
					})
				}
			}
		}
		directory["lookupQuery"] = query
	}

	if len(suppliersToAdd) == 0 {
		return logProblemAndReturnResult(ctx, "failed to add any supplier from: "+strings.Join(symbols, ","))
	}

	suppliersToAdd = s.dirAdapter.FilterAndSort(ctx, suppliersToAdd, requester.CustomData, illTrans.IllTransactionData.ServiceInfo, illTrans.IllTransactionData.BillingInfo)
	if len(suppliersToAdd) == 0 {
		return logProblemAndReturnResult(ctx, "no suppliers after filtering")
	}
	var locatedSuppliers []*ill_db.LocatedSupplier
	var dirEntries = []any{}
	for i, sup := range suppliersToAdd {
		dirEntries = append(dirEntries, map[string]any{"symbol": sup.Symbol, "peerId": sup.PeerId})
		added, loopErr := s.addLocatedSupplier(ctx, illTrans.ID, ToInt32(i), sup.LocalIdentifier, sup.Symbol, sup.PeerId, sup.Selected)
		if loopErr == nil {
			locatedSuppliers = append(locatedSuppliers, added)
		} else {
			ctx.Logger().Error("failed to add supplier", "error", loopErr)
		}
	}
	directory["entries"] = dirEntries

	return events.EventStatusSuccess, &events.EventResult{
		CustomData: map[string]any{"suppliers": locatedSuppliers, "holdings": holdings, "directory": directory, "locallyAvailable": locallyAvailable},
	}
}

func (s *SupplierLocator) addLocatedSupplier(ctx extctx.ExtendedContext, transId string, ordinal int32, locId string, symbol string, peerId string, selected bool) (*ill_db.LocatedSupplier, error) {
	status := "new"
	if selected {
		status = "selected"
	}
	supplier, err := s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: transId,
		SupplierID:       peerId,
		SupplierSymbol:   symbol,
		Ordinal:          ordinal,
		SupplierStatus: pgtype.Text{
			String: status,
			Valid:  true,
		},
		LocalID: pgtype.Text{
			String: locId,
			Valid:  true,
		},
	})
	return &supplier, err
}

func (s *SupplierLocator) selectSupplier(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	suppliers, err := s.illRepo.GetLocatedSupplierByIllTransactionAndStatus(ctx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: event.IllTransactionID,
		SupplierStatus:   ill_db.SupplierStatusSelectedPg,
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
	locSup.SupplierStatus = ill_db.SupplierStatusSelectedPg
	locSup, err = s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locSup))
	if err != nil {
		return logErrorAndReturnResult(ctx, "failed to update located supplier status", err)
	}
	return events.EventStatusSuccess, &events.EventResult{
		CustomData: map[string]any{"supplierId": locSup.SupplierID},
	}
}

func logErrorAndReturnResult(ctx extctx.ExtendedContext, message string, err error) (events.EventStatus, *events.EventResult) {
	ctx.Logger().Error(message, "error", err)
	return events.EventStatusError, &events.EventResult{
		CommonEventData: events.CommonEventData{
			EventError: &events.EventError{
				Message: message,
				Cause:   err.Error(),
			},
		},
	}
}

func logProblemAndReturnResult(ctx extctx.ExtendedContext, message string) (events.EventStatus, *events.EventResult) {
	ctx.Logger().Info(message)
	return events.EventStatusProblem, &events.EventResult{
		Problem: &events.Problem{
			Kind:    "no-suppliers",
			Details: message,
		},
	}
}

func getPeerRatio(peer ill_db.Peer) float32 {
	if peer.BorrowsCount != 0 {
		return float32(peer.LoansCount) / float32(peer.BorrowsCount)
	} else {
		return math.MaxFloat32
	}
}

func ToInt32(i int) int32 {
	if i > math.MaxInt32 {
		return math.MaxInt32
	} else if i < math.MinInt32 {
		return math.MinInt32
	} else {
		return int32(i)
	}
}
