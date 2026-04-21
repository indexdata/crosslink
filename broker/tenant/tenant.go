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

type TenantResolver struct {
	illRepo                ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
	tenantToSymbol         string
}

func NewResolver() *TenantResolver {
	return &TenantResolver{}
}

func (s *TenantResolver) WithTenantToSymbol(tenantToSymbol string) *TenantResolver {
	s.tenantToSymbol = tenantToSymbol
	return s
}

func (s *TenantResolver) WithIllRepo(illRepo ill_db.IllRepo) *TenantResolver {
	s.illRepo = illRepo
	return s
}

func (s *TenantResolver) WithLookupAdapter(directoryLookupAdapter adapter.DirectoryLookupAdapter) *TenantResolver {
	s.directoryLookupAdapter = directoryLookupAdapter
	return s
}

func (s *TenantResolver) HasTenantMapping() bool {
	return s.tenantToSymbol != ""
}

func (s *TenantResolver) mapTenantToSymbol(tenant string) string {
	return strings.ReplaceAll(s.tenantToSymbol, "{tenant}", strings.ToUpper(tenant))
}

func (s *TenantResolver) getBranchSymbols(ctx common.ExtendedContext, mainSymbol string) ([]string, error) {
	if s.illRepo == nil {
		return nil, errors.New("illRepo must be not nil")
	}
	peers, _, err := s.illRepo.GetCachedPeersBySymbols(ctx, []string{mainSymbol}, s.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	if len(peers) == 0 {
		return []string{}, nil
	}
	// expect only one peer
	peer := peers[0]
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

func (s *TenantResolver) Resolve(ctx common.ExtendedContext, r *http.Request, symbol *string) (Tenant, error) {
	requestSymbol := ""
	if symbol != nil {
		requestSymbol = *symbol
	}
	if IsOkapiRequest(r) {
		if !s.HasTenantMapping() {
			return nil, errors.New("tenant mapping must be specified")
		}
		if r.Header.Get(OkapiTenantHeader) == "" {
			return nil, errors.New("header " + OkapiTenantHeader + " must be specified")
		}

		t := &okapiTenant{
			tenantResolver: s,
			mappedSymbol:   s.mapTenantToSymbol(r.Header.Get(OkapiTenantHeader)),
			ctx:            ctx,
			requestSymbol:  requestSymbol,
		}
		return t, nil
	} else {
		t := &masterTenant{
			tenantResolver: s,
			ctx:            ctx,
			requestSymbol:  requestSymbol,
		}
		return t, nil
	}
}

type Tenant interface {
	// Returns true if tenant is owner of the symbol.
	IsOwnerOf(symbol string) (bool, error)
	// Returns all symbols owned by the tenant.
	// A nil slice means the tenant is unrestricted and owns all symbols.
	GetOwnedSymbols() ([]string, error)
	// Returns the symbol specified in the current request.
	GetRequestSymbol() (string, error)
}

type okapiTenant struct {
	tenantResolver *TenantResolver
	ctx            common.ExtendedContext
	mappedSymbol   string
	requestSymbol  string
}

func (t *okapiTenant) IsOwnerOf(symbol string) (bool, error) {
	if symbol == t.mappedSymbol {
		return true, nil
	}
	symbols, err := t.GetOwnedSymbols()
	if err != nil {
		return false, err
	}
	return slices.Contains(symbols, symbol), nil
}

func (t *okapiTenant) GetOwnedSymbols() ([]string, error) {
	branchSymbols, err := t.tenantResolver.getBranchSymbols(t.ctx, t.mappedSymbol)
	if err != nil {
		return nil, err
	}
	return append([]string{t.mappedSymbol}, branchSymbols...), nil
}

func (t *okapiTenant) GetRequestSymbol() (string, error) {
	if t.requestSymbol == "" {
		return t.mappedSymbol, nil
	}
	isOwner, err := t.IsOwnerOf(t.requestSymbol)
	if err != nil {
		return "", err
	}
	if !isOwner {
		return "", errors.New("symbol is not owned by tenant")
	}
	return t.requestSymbol, nil
}

type masterTenant struct {
	tenantResolver *TenantResolver
	ctx            common.ExtendedContext
	requestSymbol  string
}

func (t *masterTenant) IsOwnerOf(symbol string) (bool, error) {
	if t.requestSymbol == "" {
		// no symbol param specified, master tenant is owner of all symbols
		return true, nil
	}
	if symbol == t.requestSymbol {
		return true, nil
	}
	symbols, err := t.GetOwnedSymbols()
	if err != nil {
		return false, err
	}
	return slices.Contains(symbols, symbol), nil
}

func (t *masterTenant) GetOwnedSymbols() ([]string, error) {
	if t.requestSymbol == "" {
		// no symbol restriction for the master tenant
		return nil, nil
	}
	branchSymbols, err := t.tenantResolver.getBranchSymbols(t.ctx, t.requestSymbol)
	if err != nil {
		return nil, err
	}
	return append([]string{t.requestSymbol}, branchSymbols...), nil
}

func (t *masterTenant) GetRequestSymbol() (string, error) {
	return t.requestSymbol, nil
}
