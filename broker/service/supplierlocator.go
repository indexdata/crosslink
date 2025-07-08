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

const ROTA_INFO_KEY = "rotaInfo"

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

	holdingsSymbols := make([]string, 0, len(holdings))
	symbolToLocalId := make(map[string]string, len(holdings))
	potentialSuppliers := make([]adapter.Supplier, 0, len(holdings))
	var directory = map[string]any{}
	for _, holding := range holdings {
		holdingsSymbols = append(holdingsSymbols, holding.Symbol)
		symbolToLocalId[holding.Symbol] = holding.LocalIdentifier
	}

	peers, query, err := s.illRepo.GetCachedPeersBySymbols(ctx, holdingsSymbols, s.dirAdapter)
	if err != nil {
		directory["error"] = err.Error()
	} else {
		for _, peer := range peers {
			peerSymbols, err := s.illRepo.GetSymbolsByPeerId(ctx, peer.ID)
			if err != nil {
				return logErrorAndReturnResult(ctx, "failed to read symbols", err)
			}
			var symbols = []string{}
			for _, sym := range peerSymbols {
				symbols = append(symbols, sym.SymbolValue)
			}
			branchSymbols, err := s.illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
			if err != nil {
				return logErrorAndReturnResult(ctx, "failed to read branch symbols", err)
			}
			for _, sym := range branchSymbols {
				symbols = append(symbols, sym.SymbolValue)
			}
			for _, sym := range symbols {
				if localId, ok := symbolToLocalId[sym]; ok {
					local := false
					if s.localSupply &&
						illTrans.RequesterSymbol.Valid && sym == illTrans.RequesterSymbol.String {
						local = true
					}
					potentialSuppliers = append(potentialSuppliers, adapter.Supplier{
						PeerId:          peer.ID,
						CustomData:      peer.CustomData,
						LocalIdentifier: localId,
						Ratio:           getPeerRatio(peer),
						Symbol:          sym,
						Local:           local,
					})
				}
			}
		}
	}
	directory["lookupQuery"] = query

	if len(potentialSuppliers) == 0 {
		return logProblemAndReturnResult(ctx, "failed to add any supplier from: "+strings.Join(holdingsSymbols, ","))
	}

	var rotaInfo adapter.RotaInfo
	potentialSuppliers, rotaInfo = s.dirAdapter.FilterAndSort(ctx, potentialSuppliers, requester.CustomData, illTrans.IllTransactionData.ServiceInfo, illTrans.IllTransactionData.BillingInfo)
	if len(potentialSuppliers) == 0 {
		return logProblemAndReturnResultMatch(ctx, "no suppliers after filtering", rotaInfo)
	}
	var locatedSuppliers []*ill_db.LocatedSupplier
	var dirEntries = []any{}
	for i, sup := range potentialSuppliers {
		dirEntries = append(dirEntries, map[string]any{"symbol": sup.Symbol, "peerId": sup.PeerId})
		added, loopErr := s.addLocatedSupplier(ctx, illTrans.ID, ToInt32(i), &sup)
		if loopErr == nil {
			locatedSuppliers = append(locatedSuppliers, added)
		} else {
			ctx.Logger().Error("failed to add supplier", "error", loopErr)
		}
	}
	directory["entries"] = dirEntries

	return events.EventStatusSuccess, &events.EventResult{
		CustomData: map[string]any{"suppliers": locatedSuppliers, "holdings": holdings, "directory": directory, ROTA_INFO_KEY: rotaInfo},
	}
}

func (s *SupplierLocator) addLocatedSupplier(ctx extctx.ExtendedContext, transId string, ordinal int32, supplier *adapter.Supplier) (*ill_db.LocatedSupplier, error) {
	sup, err := s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: transId,
		SupplierID:       supplier.PeerId,
		SupplierSymbol:   supplier.Symbol,
		Ordinal:          ordinal,
		SupplierStatus: pgtype.Text{
			String: "new",
			Valid:  true,
		},
		LocalID: pgtype.Text{
			String: supplier.LocalIdentifier,
			Valid:  true,
		},
		LocalSupplier: supplier.Local,
	})
	return &sup, err
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
		CustomData: map[string]any{"supplierId": locSup.SupplierID, "supplierSymbol": locSup.SupplierSymbol, "localSupplier": locSup.LocalSupplier},
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
	ctx.Logger().Debug("supplier_locator: " + message)
	return events.EventStatusProblem, &events.EventResult{
		Problem: &events.Problem{
			Kind:    "no-suppliers",
			Details: message,
		},
	}
}

func logProblemAndReturnResultMatch(ctx extctx.ExtendedContext, message string, rotaInfo adapter.RotaInfo) (events.EventStatus, *events.EventResult) {
	evStatus, evResult := logProblemAndReturnResult(ctx, message)
	evResult.CustomData = map[string]any{ROTA_INFO_KEY: rotaInfo}
	return evStatus, evResult
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
