package service

import (
	"math"
	"sort"
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
	suppliersToAdd := make([]SupplierToAdd, 0, len(holdings))
	for _, holding := range holdings {
		symbols = append(symbols, holding.Symbol)
		symLocalIdMapping[holding.Symbol] = holding.LocalIdentifier
	}

	peers := s.illRepo.GetCachedPeersBySymbols(ctx, symbols, s.dirAdapter)
	for _, peer := range peers {
		if localId, ok := symLocalIdMapping[peer.Symbol]; ok {
			suppliersToAdd = append(suppliersToAdd, SupplierToAdd{
				Peer:            peer,
				LocalIdentifier: localId,
				Ratio:           getPeerRatio(peer),
			})
		} else {
			ctx.Logger().Error("could not find local id for symbol", "symbol", peer.Symbol)
		}
	}

	if len(suppliersToAdd) == 0 {
		return logProblemAndReturnResult(ctx, "failed to add any supplier from: "+strings.Join(symbols, ","))
	}

	sort.Slice(suppliersToAdd, func(i, j int) bool {
		return suppliersToAdd[i].Ratio < suppliersToAdd[j].Ratio
	})
	var locatedSuppliers []*ill_db.LocatedSupplier
	for i, sup := range suppliersToAdd {
		added, loopErr := s.addLocatedSupplier(ctx, illTrans.ID, ToInt32(i), sup.LocalIdentifier, sup.Peer)
		if loopErr == nil {
			locatedSuppliers = append(locatedSuppliers, added)
		} else {
			ctx.Logger().Error("failed to add supplier", "error", loopErr)
		}
	}

	return events.EventStatusSuccess, &events.EventResult{
		CustomData: map[string]any{"suppliers": locatedSuppliers},
	}
}

func (s *SupplierLocator) addLocatedSupplier(ctx extctx.ExtendedContext, transId string, ordinal int32, locId string, peer ill_db.Peer) (*ill_db.LocatedSupplier, error) {
	supplier, err := s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: transId,
		SupplierID:       peer.ID,
		Ordinal:          ordinal,
		SupplierStatus: pgtype.Text{
			String: "new",
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

type SupplierToAdd struct {
	LocalIdentifier string
	Peer            ill_db.Peer
	Ratio           float32
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
