package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

type TenantContext struct {
	illRepo                ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
	tenantSymbolMap        string
}

func NewTenantContext() *TenantContext {
	return &TenantContext{}
}

func (s *TenantContext) WithTenantSymbol(tenantSymbol string) *TenantContext {
	s.tenantSymbolMap = tenantSymbol
	return s
}

func (s *TenantContext) WithIllRepo(illRepo ill_db.IllRepo) *TenantContext {
	s.illRepo = illRepo
	return s
}

func (s *TenantContext) WithLookupAdapter(directoryLookupAdapter adapter.DirectoryLookupAdapter) *TenantContext {
	s.directoryLookupAdapter = directoryLookupAdapter
	return s
}

func (s *TenantContext) isSpecified() bool {
	return s.tenantSymbolMap != ""
}

func (s *TenantContext) getSymbol(tenant string) string {
	return strings.ReplaceAll(s.tenantSymbolMap, "{tenant}", strings.ToUpper(tenant))
}

type Tenant struct {
	tenantContext *TenantContext
	ctx           common.ExtendedContext
	okapiEndpoint bool
	tenant        string
	symbol        string
}

func (s *TenantContext) WithRequest(ctx common.ExtendedContext, r *http.Request, symbol *string) *Tenant {
	var pSymbol string
	if symbol != nil {
		pSymbol = *symbol
	}
	t := &Tenant{
		tenantContext: s,
		tenant:        r.Header.Get("X-Okapi-Tenant"),
		ctx:           ctx,
		okapiEndpoint: strings.HasPrefix(r.URL.Path, "/broker/"),
		symbol:        pSymbol,
	}
	return t
}

func (t *Tenant) GetSymbol() (string, error) {
	var mainSymbol string
	if t.okapiEndpoint {
		if !t.tenantContext.isSpecified() {
			return "", errors.New("tenant mapping must be specified")
		}
		if t.tenant == "" {
			return "", errors.New("header X-Okapi-Tenant must be specified")
		}
		mainSymbol = t.tenantContext.getSymbol(t.tenant)
	} else {
		if t.symbol == "" {
			return "", errors.New("symbol must be specified")
		}
		mainSymbol = t.symbol
	}
	if t.tenantContext.illRepo == nil {
		return mainSymbol, nil
	}
	peers, _, err := t.tenantContext.illRepo.GetCachedPeersBySymbols(t.ctx, []string{mainSymbol}, t.tenantContext.directoryLookupAdapter)
	if err != nil {
		return "", err
	}
	// seems like len(peers) > 0 always, so we can't flag an error for symbol that does not exist
	// if supplied symbol is the same as main symbol, we can skip the check against branch symbols, since it's valid
	if t.symbol == "" || t.symbol == mainSymbol {
		return mainSymbol, nil
	}
	found := false
	for _, peer := range peers {
		branchSymbols, err := t.tenantContext.illRepo.GetBranchSymbolsByPeerId(t.ctx, peer.ID)
		if err != nil {
			return "", err
		}
		for _, branchSymbol := range branchSymbols {
			if t.symbol == branchSymbol.SymbolValue {
				found = true
			}
		}
	}
	if !found {
		return "", errors.New("symbol does not match any branch symbols for tenant")
	}
	return t.symbol, nil
}

// GetSymbols returns the main symbol for the tenant and all branch symbols of peers associated with that symbol.
// Note that empty list and nil are used to distinguish between "no symbols" and "all symbols" (i.e. no symbol filtering).
func (t *Tenant) GetSymbols() ([]string, error) {
	var mainSymbol string
	if t.okapiEndpoint {
		if !t.tenantContext.isSpecified() {
			return nil, errors.New("tenant mapping must be specified")
		}
		if t.tenant == "" {
			return nil, errors.New("header X-Okapi-Tenant must be specified")
		}
		mainSymbol = t.tenantContext.getSymbol(t.tenant)
	} else {
		if t.symbol == "" {
			return nil, nil
		}
		mainSymbol = t.symbol
	}
	allSyms := []string{mainSymbol}
	if t.tenantContext.illRepo == nil {
		return allSyms, nil
	}
	peers, _, err := t.tenantContext.illRepo.GetCachedPeersBySymbols(t.ctx, []string{mainSymbol}, t.tenantContext.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		branchSymbols, err := t.tenantContext.illRepo.GetBranchSymbolsByPeerId(t.ctx, peer.ID)
		if err != nil {
			return nil, err
		}
		for _, branchSymbol := range branchSymbols {
			allSyms = append(allSyms, branchSymbol.SymbolValue)
		}
	}
	return allSyms, nil
}
