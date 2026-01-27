package service

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
)

const COMP = "supplier_locator"
const SUP_PROBLEM = "no-suppliers"
const ROTA_INFO_KEY = "rotaInfo"
const DATE_LAYOUT = "2006-01-02"

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

func (s *SupplierLocator) LocateSuppliers(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	_, _ = s.eventBus.ProcessTask(ctx, event, s.locateSuppliers)
}

func (s *SupplierLocator) SelectSupplier(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	_, _ = s.eventBus.ProcessTask(ctx, event, s.selectSupplier)
}

func (s *SupplierLocator) locateSuppliers(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	illTrans, err := s.illRepo.GetIllTransactionById(ctx, event.IllTransactionID)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to read ILL transaction", err)
	}

	if illTrans.IllTransactionData.BibliographicInfo.SupplierUniqueRecordId == "" {
		return events.LogProblemAndReturnResult(ctx, SUP_PROBLEM, "ILL transaction missing SupplierUniqueRecordId", nil)
	}

	requester, err := s.illRepo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to read requester peer", err)
	}

	holdings, query, err := s.holdingsAdapter.Lookup(adapter.HoldingLookupParams{
		Identifier: illTrans.IllTransactionData.BibliographicInfo.SupplierUniqueRecordId,
	})
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, fmt.Sprintf("failed to locate holdings for query '%s'", query), err)
	}
	var holdingsLog = map[string]any{}
	holdingsLog["lookupQuery"] = query
	if len(holdings) == 0 {
		return events.LogProblemAndReturnResult(ctx, SUP_PROBLEM, "no holdings located",
			map[string]any{"holdings": holdingsLog, "supplierUniqueRecordId": illTrans.IllTransactionData.BibliographicInfo.SupplierUniqueRecordId})
	}
	holdingsLog["entries"] = holdings
	holdingsSymbols := make([]string, 0, len(holdings))
	symbolToLocalId := make(map[string]string, len(holdings))
	potentialSuppliers := make([]adapter.Supplier, 0, len(holdings))
	for _, holding := range holdings {
		holdingsSymbols = append(holdingsSymbols, holding.Symbol)
		symbolToLocalId[holding.Symbol] = holding.LocalIdentifier
	}
	peers, query, err := s.illRepo.GetCachedPeersBySymbols(ctx, holdingsSymbols, s.dirAdapter)
	var directoryLog = map[string]any{}
	directoryLog["lookupQuery"] = query
	if err != nil {
		directoryLog["error"] = err.Error()
	}
	if len(peers) > 0 { //even with lookup error we may have locally cached peers
		var dirEntriesLog = []any{}
		for _, peer := range peers {
			peerSymbols, err := s.illRepo.GetSymbolsByPeerId(ctx, peer.ID)
			if err != nil {
				return events.LogErrorAndReturnResult(ctx, "failed to read symbols", err)
			}
			var symbols = []string{}
			symbolsLog := ""
			sep := ""
			for _, sym := range peerSymbols {
				symbols = append(symbols, sym.SymbolValue)
				symbolsLog += sep + sym.SymbolValue
				sep = ", "
			}
			// In normal case this will be giving empty list because each branch has its own peer entry,
			// but we will keep this check because of flexibility
			branchSymbols, err := s.illRepo.GetExclusiveBranchSymbolsByPeerId(ctx, peer.ID)
			if err != nil {
				return events.LogErrorAndReturnResult(ctx, "failed to read branch symbols", err)
			}
			branchSymbolsLog := ""
			sep = ""
			for _, sym := range branchSymbols {
				symbols = append(symbols, sym.SymbolValue)
				branchSymbolsLog += sep + sym.SymbolValue
				sep = ", "
			}
			dirEntriesLog = append(dirEntriesLog, map[string]any{"id": peer.ID, "name": peer.Name, "symbols": symbolsLog, "branchSymbols": branchSymbolsLog})
			for _, sym := range symbols {
				if localId, ok := symbolToLocalId[sym]; ok {
					local := false
					supplierStatus := ill_db.SupplierStateNewPg
					if illTrans.RequesterSymbol.Valid && sym == illTrans.RequesterSymbol.String {
						if requester.BrokerMode == string(common.BrokerModeOpaque) {
							supplierStatus = ill_db.SupplierStateSkippedPg // Skip local supplier
						} else {
							local = true
						}
					}
					potentialSuppliers = append(potentialSuppliers, adapter.Supplier{
						PeerId:          peer.ID,
						CustomData:      peer.CustomData,
						LocalIdentifier: localId,
						Ratio:           getPeerRatio(peer),
						Symbol:          sym,
						Local:           local,
						SupplierStatus:  supplierStatus,
					})
				}
			}
		}
		directoryLog["entries"] = dirEntriesLog
	}
	if len(potentialSuppliers) == 0 {
		return events.LogProblemAndReturnResult(ctx, SUP_PROBLEM, "no suppliers located",
			map[string]any{"holdings": holdingsLog, "directory": directoryLog})
	}
	var rotaInfo adapter.RotaInfo
	potentialSuppliers, rotaInfo = s.dirAdapter.FilterAndSort(ctx, potentialSuppliers, requester.CustomData,
		illTrans.IllTransactionData.ServiceInfo, illTrans.IllTransactionData.BillingInfo)
	if len(potentialSuppliers) == 0 {
		return events.LogProblemAndReturnResult(ctx, SUP_PROBLEM, "no located suppliers match",
			map[string]any{"holdings": holdingsLog, "directory": directoryLog, ROTA_INFO_KEY: rotaInfo})
	}
	var locatedSuppliers []*ill_db.LocatedSupplier
	for i, sup := range potentialSuppliers {
		added, loopErr := s.addLocatedSupplier(ctx, illTrans.ID, ToInt32(i), &sup)
		if loopErr == nil {
			locatedSuppliers = append(locatedSuppliers, added)
		} else {
			ctx.Logger().Error("failed to add supplier", "error", loopErr)
		}
	}

	return events.EventStatusSuccess, &events.EventResult{
		CustomData: map[string]any{"suppliers": locatedSuppliers, "holdings": holdingsLog, "directory": directoryLog, ROTA_INFO_KEY: rotaInfo},
	}
}

func (s *SupplierLocator) addLocatedSupplier(ctx common.ExtendedContext, transId string, ordinal int32, supplier *adapter.Supplier) (*ill_db.LocatedSupplier, error) {
	sup, err := s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: transId,
		SupplierID:       supplier.PeerId,
		SupplierSymbol:   supplier.Symbol,
		Ordinal:          ordinal,
		SupplierStatus:   supplier.SupplierStatus,
		LocalID: pgtype.Text{
			String: supplier.LocalIdentifier,
			Valid:  true,
		},
		LocalSupplier: supplier.Local,
	})
	return &sup, err
}

func (s *SupplierLocator) selectSupplier(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	suppliers, err := s.illRepo.GetLocatedSuppliersByIllTransactionAndStatus(ctx, ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams{
		IllTransactionID: event.IllTransactionID,
		SupplierStatus:   ill_db.SupplierStateSelectedPg,
	})
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "could not find selected suppliers", err)
	}
	if len(suppliers) > 0 {
		for _, supplier := range suppliers {
			supplier.SupplierStatus = ill_db.SupplierStateSkippedPg
			_, err = s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(supplier))
			if err != nil {
				return events.LogErrorAndReturnResult(ctx, "could not update previous selected supplier", err)
			}
		}
	}
	suppliers, err = s.illRepo.GetLocatedSuppliersByIllTransactionAndStatus(ctx, ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams{
		IllTransactionID: event.IllTransactionID,
		SupplierStatus:   ill_db.SupplierStateNewPg,
	})
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "could not find located suppliers", err)
	}
	if len(suppliers) == 0 {
		return events.LogProblemAndReturnResult(ctx, SUP_PROBLEM, "no suppliers with new status", nil)
	}
	locSup, skippedSuppliers, err := s.getNextSupplier(ctx, suppliers)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "could not find supplier peer", err)
	}
	if locSup.ID == "" {
		eventData := map[string]any{}
		if len(skippedSuppliers) > 0 {
			eventData["skippedSuppliers"] = skippedSuppliers
		}
		return events.LogProblemAndReturnResult(ctx, SUP_PROBLEM, "no suppliers with new status", eventData)
	}
	locSup.SupplierStatus = ill_db.SupplierStateSelectedPg
	locSup, err = s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locSup))
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to update located supplier status", err)
	}
	eventData := map[string]any{"supplierId": locSup.SupplierID, "supplierSymbol": locSup.SupplierSymbol, "localSupplier": locSup.LocalSupplier}
	if len(skippedSuppliers) > 0 {
		eventData["skippedSuppliers"] = skippedSuppliers
	}
	return events.EventStatusSuccess, &events.EventResult{CustomData: eventData}
}

func (s *SupplierLocator) getNextSupplier(ctx common.ExtendedContext, suppliers []ill_db.LocatedSupplier) (ill_db.LocatedSupplier, []SkippedSupplier, error) {
	skippedSuppliers := []SkippedSupplier{}
	for _, sup := range suppliers {
		if sup.ID != "" {
			peer, err := s.illRepo.GetPeerById(ctx, sup.SupplierID)
			if err != nil {
				return ill_db.LocatedSupplier{}, skippedSuppliers, err
			}
			if peer.CustomData.Closures != nil {
				timezoneLoc := time.Now().Location()
				timeZone := peer.CustomData.TimeZone
				if timeZone != nil && *timeZone != "" {
					timezoneLoc, err = time.LoadLocation(*timeZone)
					if err != nil {
						return ill_db.LocatedSupplier{}, skippedSuppliers, err
					}
				}
				currentTime := time.Now().In(timezoneLoc)
				skipSup := false
				for _, closure := range *peer.CustomData.Closures {
					if closure.StartDate != nil && closure.EndDate != nil {
						startDate, err := getDateWithTimezone(*closure.StartDate, timezoneLoc, false)
						if err != nil {
							ctx.Logger().Error("failed to parse closure start date", "error", err)
							skipSup = true
							continue
						}
						endDate, err := getDateWithTimezone(*closure.EndDate, timezoneLoc, true)
						if err != nil {
							ctx.Logger().Error("failed to parse closure end date", "error", err)
							skipSup = true
							continue
						}
						if startDate.Before(currentTime) && endDate.After(currentTime) {
							skipSup = true
						}
					}
				}
				if skipSup {
					sup.SupplierStatus = ill_db.SupplierStateSkippedPg
					_, err = s.illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(sup))
					if err != nil {
						return ill_db.LocatedSupplier{}, skippedSuppliers, err
					}
					skippedSuppliers = append(skippedSuppliers, SkippedSupplier{
						Symbol: sup.SupplierSymbol,
						Reason: fmt.Sprintf("closed on %s", time.Now().Format("2006-01-02")),
					})
					continue
				}
			}
			return sup, skippedSuppliers, nil
		}
	}
	return ill_db.LocatedSupplier{}, skippedSuppliers, nil
}

func getDateWithTimezone(date string, loc *time.Location, endOfDay bool) (time.Time, error) {
	t, err := time.Parse(DATE_LAYOUT, date)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.Add(24 * time.Hour)
	}
	year, month, day := t.Year(), t.Month(), t.Day()
	returnDate := time.Date(year, month, day,
		0, 0, 0, 0, // Hour, Minute, Second, Nanosecond (set to midnight)
		loc,
	)
	if endOfDay {
		returnDate = returnDate.Add(-time.Nanosecond)
	}
	return returnDate, nil
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

type SkippedSupplier struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
}
