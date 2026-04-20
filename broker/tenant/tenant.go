package tenant

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

const OKAPI_PATH_PREFIX = "/broker"
const OkapiTenantHeader = "X-Okapi-Tenant"

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

func (s *TenantContext) mapSymbol(tenant string) string {
	return strings.ReplaceAll(s.tenantSymbolMap, "{tenant}", strings.ToUpper(tenant))
}

func (s *TenantContext) getBranchSymbols(ctx common.ExtendedContext, mainSymbol string) ([]string, error) {
	if s.illRepo == nil {
		return nil, errors.New("illRepo must be not nil")
	}
	peers, _, err := s.illRepo.GetCachedPeersBySymbols(ctx, []string{mainSymbol}, s.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		//expect only one peer
		branchSymbols, err := s.illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
		if err != nil {
			return nil, err
		}
		symbols := make([]string, 0, len(branchSymbols))
		for _, branchSymbol := range branchSymbols {
			symbols = append(symbols, branchSymbol.SymbolValue)
		}
		return symbols, nil
	}
	return []string{}, nil
}

func (s *TenantContext) WithRequest(ctx common.ExtendedContext, r *http.Request, symbol *string) (Tenant, error) {
	if IsOkapiRequest(r) {
		if !s.IsSpecified() {
			return nil, errors.New("tenant mapping must be specified")
		}
		if r.Header.Get(OkapiTenantHeader) == "" {
			return nil, errors.New("header " + OkapiTenantHeader + " must be specified")
		}

		t := &okapiTenant{
			tenantContext: s,
			tenantHeader:  r.Header.Get(OkapiTenantHeader),
			mappedSymbol:  s.mapSymbol(r.Header.Get(OkapiTenantHeader)),
			ctx:           ctx,
		}
		return t, nil
	} else {
		t := &masterTenant{
			tenantContext: s,
			ctx:           ctx,
			symbolParam:   symbol,
		}
		return t, nil
	}
}

type Tenant interface {
	// Returns trues if tenant is owner of the symbol,
	IsOwnerOf(symbol string) (bool, error)
	// Return the primary symbol
	GetSymbol() (string, error)
	// Returns all owned symbols, or error if symbols cannot be determined
	GetSymbols() ([]string, error)
}

type okapiTenant struct {
	tenantContext *TenantContext
	ctx           common.ExtendedContext
	mappedSymbol  string
	tenantHeader  string
}

func (t *okapiTenant) IsOwnerOf(symbol string) (bool, error) {
	if t.mappedSymbol == symbol {
		return true, nil
	}
	branchSymbols, err := t.tenantContext.getBranchSymbols(t.ctx, t.mappedSymbol)
	if err != nil {
		return false, err
	}
	return slices.Contains(branchSymbols, symbol), nil
}

func (t *okapiTenant) GetSymbol() (string, error) {
	return t.mappedSymbol, nil
}

func (t *okapiTenant) GetSymbols() ([]string, error) {
	branchSymbols, err := t.tenantContext.getBranchSymbols(t.ctx, t.mappedSymbol)
	if err != nil {
		return nil, err
	}
	return append([]string{t.mappedSymbol}, branchSymbols...), nil
}

type masterTenant struct {
	tenantContext *TenantContext
	ctx           common.ExtendedContext
	symbolParam   *string
}

func (t *masterTenant) IsOwnerOf(symbol string) (bool, error) {
	if t.symbolParam == nil || *t.symbolParam == "" {
		//no symbol param specified, master tenant is owner of all symbols
		return true, nil
	}
	if *t.symbolParam == symbol {
		return true, nil
	}
	branchSymbols, err := t.tenantContext.getBranchSymbols(t.ctx, *t.symbolParam)
	if err != nil {
		return false, err
	}
	return slices.Contains(branchSymbols, symbol), nil
}

func (t *masterTenant) GetSymbols() ([]string, error) {
	if t.symbolParam == nil || *t.symbolParam == "" {
		// No symbol restriction for the master tenant.
		return nil, nil
	}
	branchSymbols, err := t.tenantContext.getBranchSymbols(t.ctx, *t.symbolParam)
	if err != nil {
		return nil, err
	}
	return append([]string{*t.symbolParam}, branchSymbols...), nil
}

func (t *masterTenant) GetSymbol() (string, error) {
	if t.symbolParam == nil || *t.symbolParam == "" {
		return "", errors.New("symbol must be specified")
	}
	return *t.symbolParam, nil
}
