package tenant

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
)

const OKAPI_PATH_PREFIX = "/broker"
const OkapiTenantHeader = "X-Okapi-Tenant"

//nolint:gosec // Header name, not a hardcoded credential.
const OkapiTokenHeader = "X-Okapi-Token"
const OkapiUserHeader = "X-Okapi-User-Id"
const XForwardedForHeader = "X-Forwarded-For"
const XForwardedUserHeader = "X-Forwarded-User"

const MapToSymbolDirectory = "directory"
const maxAgeDefault = time.Duration(5 * time.Minute)

func IsOkapiRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, OKAPI_PATH_PREFIX+"/")
}

type cacheEntry struct {
	symbols    []string
	expiration time.Time
}

type TenantResolver struct {
	illRepo                ill_db.IllRepo
	directoryLookupAdapter adapter.DirectoryLookupAdapter
	tenantToSymbol         string
	tenantSymbolsMap       sync.Map
	maxAge                 time.Duration
}

func NewResolver() *TenantResolver {
	return &TenantResolver{
		maxAge: maxAgeDefault,
	}
}

func (s *TenantResolver) WithTenantToSymbol(tenantToSymbol string) *TenantResolver {
	s.tenantToSymbol = tenantToSymbol
	return s
}

func (s *TenantResolver) WithIllRepo(illRepo ill_db.IllRepo) *TenantResolver {
	s.illRepo = illRepo
	return s
}

func (s *TenantResolver) WithMaxAge(maxAge time.Duration) *TenantResolver {
	s.maxAge = maxAge
	return s
}

func (s *TenantResolver) WithLookupAdapter(directoryLookupAdapter adapter.DirectoryLookupAdapter) *TenantResolver {
	s.directoryLookupAdapter = directoryLookupAdapter
	return s
}

func (s *TenantResolver) HasTenantMapping() bool {
	return s.tenantToSymbol != ""
}

func (s *TenantResolver) clearStale() {
	now := time.Now()
	s.tenantSymbolsMap.Range(func(key, value any) bool {
		entry := value.(*cacheEntry)
		if now.After(entry.expiration) {
			s.tenantSymbolsMap.Delete(key)
		}
		return true
	})
}

func (s *TenantResolver) mapTenantToSymbols(ctx common.ExtendedContext, tenant string) ([]string, error) {
	if s.tenantToSymbol != MapToSymbolDirectory {
		return []string{strings.ReplaceAll(s.tenantToSymbol, "{tenant}", strings.ToUpper(tenant))}, nil
	}
	// could use DB for caching instead of in-memory map if we want to share cache across
	// instances or persist it, but in-memory should be sufficient for now and is simpler.
	s.clearStale()
	if v, ok := s.tenantSymbolsMap.Load(tenant); ok {
		entry := v.(*cacheEntry)
		return entry.symbols, nil
	}
	if s.directoryLookupAdapter == nil {
		return nil, errors.New("directoryLookupAdapter must not be nil for tenant to symbol lookup")
	}
	entries, _, err := s.directoryLookupAdapter.Lookup(ctx, adapter.DirectoryLookupParams{Tenant: tenant})
	if err != nil {
		return nil, err
	}
	var symbols []string
	for _, entry := range entries {
		if entry.Symbols != nil {
			symbols = append(symbols, entry.Symbols...)
		}
	}
	if len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols found in directory entries for tenant %s", tenant)
	}
	s.tenantSymbolsMap.Store(tenant, &cacheEntry{symbols: symbols, expiration: time.Now().Add(s.maxAge)})
	return symbols, nil
}

func (s *TenantResolver) getBranchSymbols(ctx common.ExtendedContext, mainSymbols []string) ([]string, error) {
	if s.illRepo == nil {
		return nil, errors.New("illRepo must not be nil")
	}
	peers, _, err := s.illRepo.GetCachedPeersBySymbols(ctx, mainSymbols, s.directoryLookupAdapter)
	if err != nil {
		return nil, err
	}
	var symbols []string
	for _, peer := range peers {
		branchSymbols, err := s.illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
		if err != nil {
			return nil, err
		}
		for _, branchSymbol := range branchSymbols {
			symbols = append(symbols, branchSymbol.SymbolValue)
		}
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
		symbols, err := s.mapTenantToSymbols(ctx, tenantHeader)
		if err != nil {
			return nil, fmt.Errorf("failed to map tenant to symbol: %w", err)
		}
		t := &okapiTenant{
			tenantResolver: s,
			mappedSymbols:  append([]string(nil), symbols...),
			ctx:            ctx,
			requestSymbol:  requestSymbol,
			user:           getOkapiUser(r.Header),
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
	// Returns the user value associated with the request.
	// The returned format is implementation-specific and may include remote host
	// information; callers that need the host/IP separately should use
	// GetRemoteHost().
	GetUser() string
	// Returns remote host/IP associated with the request.
	GetRemoteHost() string
}

type okapiTenant struct {
	tenantResolver *TenantResolver
	ctx            common.ExtendedContext
	mappedSymbols  []string
	requestSymbol  string
	user           string
	remoteHost     string
}

func (t *okapiTenant) IsOwnerOf(symbol string) (bool, error) {
	if slices.Contains(t.mappedSymbols, symbol) {
		return true, nil
	}
	symbols, err := t.GetOwnedSymbols()
	if err != nil {
		return false, err
	}
	return slices.Contains(symbols, symbol), nil
}

func (t *okapiTenant) GetOwnedSymbols() ([]string, error) {
	branchSymbols, err := t.tenantResolver.getBranchSymbols(t.ctx, t.mappedSymbols)
	if err != nil {
		return nil, err
	}
	var combined []string
	combined = append(combined, t.mappedSymbols...)
	if branchSymbols != nil {
		combined = append(combined, branchSymbols...)
	}
	return combined, nil
}

func (t *okapiTenant) GetRequestSymbol() (string, error) {
	if t.requestSymbol == "" {
		return t.mappedSymbols[0], nil
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
	branchSymbols, err := t.tenantResolver.getBranchSymbols(t.ctx, []string{t.requestSymbol})
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

func getOkapiUser(header http.Header) string {
	// Okapi/auth validates the token before forwarding; this decode is only for attribution.
	if user := getOkapiTokenSubject(header.Get(OkapiTokenHeader)); user != "" {
		return user
	}
	return strings.TrimSpace(header.Get(OkapiUserHeader))
}

func getOkapiTokenSubject(token string) string {
	_, payload, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok {
		return ""
	}
	payload, _, ok = strings.Cut(payload, ".")
	if !ok {
		return ""
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims struct {
		Subject string `json:"sub"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Subject)
}
