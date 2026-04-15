package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type SymbolChecker struct {
	illRepo                *ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
	tenantSymbolMap        string
}

func NewSymbolChecker() *SymbolChecker {
	return &SymbolChecker{}
}

func (s *SymbolChecker) WithTenantSymbol(tenantSymbol string) *SymbolChecker {
	s.tenantSymbolMap = tenantSymbol
	return s
}

func (s *SymbolChecker) WithIllRepo(illRepo ill_db.IllRepo) *SymbolChecker {
	s.illRepo = &illRepo
	return s
}

func (s *SymbolChecker) WithLookupAdapter(directoryLookupAdapter adapter.DirectoryLookupAdapter) *SymbolChecker {
	s.directoryLookupAdapter = directoryLookupAdapter
	return s
}

func (s *SymbolChecker) IsSpecified() bool {
	return s.tenantSymbolMap != ""
}

func (s *SymbolChecker) getSymbol(tenant string) string {
	return strings.ReplaceAll(s.tenantSymbolMap, "{tenant}", strings.ToUpper(tenant))
}

func (s *SymbolChecker) symbolForRequest(ctx common.ExtendedContext, isBrokerPrefix bool, tenant *string, symbol *string) (string, error) {
	var mainSymbol string
	if isBrokerPrefix {
		if !s.IsSpecified() {
			return "", errors.New("tenant mapping must be specified")
		}
		if tenant == nil || *tenant == "" {
			return "", errors.New("X-Okapi-Tenant must be specified")
		}
		mainSymbol = s.getSymbol(*tenant)
	} else {
		if symbol == nil || *symbol == "" {
			return "", errors.New("symbol must be specified")
		}
		mainSymbol = *symbol
	}
	if s.illRepo == nil {
		return mainSymbol, nil
	}
	peers, _, err := (*s.illRepo).GetCachedPeersBySymbols(ctx, []string{mainSymbol}, s.directoryLookupAdapter)
	if err != nil {
		return "", err
	}
	if len(peers) == 0 {
		ctx.Logger().Error("no peers for symbol", "symbol", mainSymbol)
		return "", errors.New("no peers for symbol")
	}
	if symbol == nil || *symbol == "" || *symbol == mainSymbol {
		return mainSymbol, nil
	}
	found := false
	for _, peer := range peers {
		branchSymbols, err := (*s.illRepo).GetBranchSymbolsByPeerId(ctx, peer.ID)
		if err != nil {
			return "", err
		}
		for _, branchSymbol := range branchSymbols {
			if *symbol == branchSymbol.SymbolValue {
				found = true
				break
			}
		}
	}
	if !found {
		return "", errors.New("symbol does not match any branch symbols for tenant")
	}
	return *symbol, nil
}

func (s *SymbolChecker) GetSymbolForRequest(ctx common.ExtendedContext, r *http.Request, tenant *string, symbol *string) (string, error) {
	return s.symbolForRequest(ctx, strings.HasPrefix(r.URL.Path, "/broker/"), tenant, symbol)
}

func (s *SymbolChecker) GetSymbolsForTenant(ctx common.ExtendedContext, tenant string) []string {
	if !s.IsSpecified() {
		return []string{}
	}
	if tenant == "" {
		return []string{}
	}
	mainSymbol := s.getSymbol(tenant)
	if s.illRepo == nil {
		return []string{mainSymbol}
	}
	peers, _, err := (*s.illRepo).GetCachedPeersBySymbols(ctx, []string{mainSymbol}, s.directoryLookupAdapter)
	if err != nil {
		return []string{}
	}
	if len(peers) == 0 {
		ctx.Logger().Error("no peers for symbol", "symbol", mainSymbol)
		return []string{}
	}
	symbols := []string{mainSymbol}
	for _, peer := range peers {
		branchSymbols, err := (*s.illRepo).GetBranchSymbolsByPeerId(ctx, peer.ID)
		if err != nil {
			return []string{}
		}
		for _, branchSymbol := range branchSymbols {
			symbols = append(symbols, branchSymbol.SymbolValue)
		}
	}
	return symbols
}
