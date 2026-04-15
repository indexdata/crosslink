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
	tenantResolver         common.Tenant
}

func NewSymbolChecker(tenantResolver common.Tenant) *SymbolChecker {
	return &SymbolChecker{
		tenantResolver: tenantResolver,
	}
}

func (s *SymbolChecker) WithIllRepo(illRepo ill_db.IllRepo) *SymbolChecker {
	s.illRepo = &illRepo
	return s
}

func (s *SymbolChecker) WithLookupAdapter(directoryLookupAdapter adapter.DirectoryLookupAdapter) *SymbolChecker {
	s.directoryLookupAdapter = directoryLookupAdapter
	return s
}

func (s *SymbolChecker) Check(ctx common.ExtendedContext, isBrokerPrefix bool, tenant *string, symbol *string) (string, error) {
	var mainSymbol string
	if isBrokerPrefix {
		if !s.tenantResolver.IsSpecified() {
			return "", errors.New("tenant mapping must be specified")
		}
		if tenant == nil || *tenant == "" {
			return "", errors.New("X-Okapi-Tenant must be specified")
		}
		mainSymbol = s.tenantResolver.GetSymbol(*tenant)
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
	if symbol == nil || *symbol == mainSymbol {
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

func (s *SymbolChecker) GetSymbolsForTenant(ctx common.ExtendedContext, tenant string) []string {
	if !s.tenantResolver.IsSpecified() {
		return []string{}
	}
	if tenant == "" {
		return []string{}
	}
	mainSymbol := s.tenantResolver.GetSymbol(tenant)
	if s.illRepo == nil {
		return []string{mainSymbol}
	}
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

func (s *SymbolChecker) GetSymbolForRequest(ctx common.ExtendedContext, r *http.Request, tenant *string, symbol *string) (string, error) {
	return s.Check(ctx, strings.HasPrefix(r.URL.Path, "/broker/"), tenant, symbol)
}
