package tenant

import (
	"errors"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

const OKAPI_PATH_PREFIX = "/broker"

func IsOkapiRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, OKAPI_PATH_PREFIX+"/")
}

type TenantContext struct {
	illRepo                ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
	tenantSymbolMap        string
}

func NewContext() *TenantContext {
	return &TenantContext{}
}

func (s *TenantContext) WithTenantSymbolMap(tenantSymbolMap string) *TenantContext {
	s.tenantSymbolMap = tenantSymbolMap
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

func (s *TenantContext) IsSpecified() bool {
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
		okapiEndpoint: IsOkapiRequest(r),
		symbol:        pSymbol,
	}
	return t
}

func (t *Tenant) GetSymbol() (string, error) {
	var mainSymbol string
	if t.okapiEndpoint {
		if !t.tenantContext.IsSpecified() {
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
	// if supplied symbol is the same as main symbol, we can skip the check against branch symbols, since it's valid
	// we do not check even if only one symbol because GetCachedPeersBySymbols() creates a peer for the main symbol if it does not exist,
	// so we would not be able to distinguish between "symbol does not exist" and "symbol exists but has no peers"
	if t.symbol == "" || t.symbol == mainSymbol {
		return mainSymbol, nil
	}
	peers, _, err := t.tenantContext.illRepo.GetCachedPeersBySymbols(t.ctx, []string{mainSymbol}, t.tenantContext.directoryLookupAdapter)
	if err != nil {
		return "", err
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
// A nil slice means no symbol filtering should be applied. Otherwise, the returned slice contains at least the
// main symbol and may include associated branch symbols.
func (t *Tenant) GetSymbols() ([]string, error) {
	var mainSymbol string
	if t.okapiEndpoint {
		if !t.tenantContext.IsSpecified() {
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
