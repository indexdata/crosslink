package tenant

import (
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

const OKAPI_PATH_PREFIX = "/broker"
const OkapiTenantHeader = "X-Okapi-Tenant"
const OkapiUserHeader = "X-Okapi-User-Id"
const XForwardedForHeader = "X-Forwarded-For"
const XForwardedUserHeader = "X-Forwarded-User"

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
		return nil, errors.New("illRepo must not be nil")
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
		requestSymbol = strings.TrimSpace(*symbol)
	}
	if IsOkapiRequest(r) {
		if !s.HasTenantMapping() {
			return nil, errors.New("tenant mapping must be specified")
		}
		tenantHeader := strings.TrimSpace(r.Header.Get(OkapiTenantHeader))
		if tenantHeader == "" {
			return nil, errors.New("header " + OkapiTenantHeader + " must be specified")
		}

		t := &okapiTenant{
			tenantResolver: s,
			mappedSymbol:   s.mapTenantToSymbol(tenantHeader),
			ctx:            ctx,
			requestSymbol:  requestSymbol,
			user:           strings.TrimSpace(r.Header.Get(OkapiUserHeader)),
			remoteHost:     getRemoteHost(r),
		}
		return t, nil
	} else {
		t := &masterTenant{
			tenantResolver: s,
			ctx:            ctx,
			requestSymbol:  requestSymbol,
			user:           strings.TrimSpace(r.Header.Get(XForwardedUserHeader)),
			remoteHost:     getRemoteHost(r),
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
	// Returns current user ID associated with the request.
	GetUser() string
	// Returns remote host/IP associated with the request.
	GetRemoteHost() string
}

type okapiTenant struct {
	tenantResolver *TenantResolver
	ctx            common.ExtendedContext
	mappedSymbol   string
	requestSymbol  string
	user           string
	remoteHost     string
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

func (t *okapiTenant) GetUser() string {
	if t.user != "" {
		return t.user
	}
	return "unknown"
}

func (t *okapiTenant) GetRemoteHost() string {
	return t.remoteHost
}

type masterTenant struct {
	tenantResolver *TenantResolver
	ctx            common.ExtendedContext
	requestSymbol  string
	user           string
	remoteHost     string
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

func (t *masterTenant) GetUser() string {
	user := t.user
	if user == "" {
		user = "unknown"
	}
	host := t.remoteHost
	if host == "" {
		host = "unknown"
	}
	return user + "@" + host
}

func (t *masterTenant) GetRemoteHost() string {
	return t.remoteHost
}

func getRemoteHost(r *http.Request) string {
	first, _, _ := strings.Cut(r.Header.Get(XForwardedForHeader), ",")
	host := strings.TrimSpace(first)
	if host != "" {
		return host
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
