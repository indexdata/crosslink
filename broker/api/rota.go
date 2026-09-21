package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	pathmatch "path"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/tenant"
)

// ConfigureManualRota enables edits for Okapi tenants matching any configured glob.
// Patterns use path.Match syntax; malformed patterns never match.
// Unprefixed administrative routes cannot bypass the tenant gate.
func (a *ApiHandler) ConfigureManualRota(directory adapter.DirectoryLookupAdapter, patterns []string) {
	a.rotaService = service.NewRotaService(a.illRepo, directory)
	a.manualRotaTenantPatterns = nil
	for _, pattern := range patterns {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			a.manualRotaTenantPatterns = append(a.manualRotaTenantPatterns, pattern)
		}
	}
}

func (a *ApiHandler) manualRotaEnabled(tenantName string) bool {
	if tenantName == "" {
		return false
	}
	for _, pattern := range a.manualRotaTenantPatterns {
		if matched, err := pathmatch.Match(pattern, tenantName); err == nil && matched {
			return true
		}
	}
	return false
}

func (a *ApiHandler) rotaTenant(ctx common.ExtendedContext, w http.ResponseWriter, r *http.Request) tenant.Tenant {
	tenantName := strings.TrimSpace(r.Header.Get(tenant.OkapiTenantHeader))
	if !tenant.IsOkapiRequest(r) || !a.manualRotaEnabled(tenantName) || a.rotaService == nil {
		WriteJsonErrorResponse(w, errors.New("manual rota editing is disabled for this tenant"), http.StatusForbidden)
		return nil
	}
	owner, err := a.tenantResolver.Resolve(ctx, r, nil)
	if err != nil {
		AddBadRequestError(ctx, w, err)
		return nil
	}
	return owner
}

// MoveLocatedSupplier applies a relative move among untried suppliers.
func (a *ApiHandler) MoveLocatedSupplier(w http.ResponseWriter, r *http.Request, id, supplierId string) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{TransactionId: id})
	owner := a.rotaTenant(ctx, w, r)
	if owner == nil {
		return
	}
	var body struct {
		Offset *int64 `json:"offset"`
	}
	if err := decodeRotaBody(w, r, &body); err != nil {
		AddBadRequestError(ctx, w, err)
		return
	}
	if body.Offset == nil {
		AddBadRequestError(ctx, w, errors.New("offset is required"))
		return
	}
	rows, err := a.rotaService.Move(ctx, id, supplierId, *body.Offset, owner)
	if err != nil {
		writeRotaError(ctx, w, err)
		return
	}
	resp := oapi.LocatedSuppliers{Items: make([]oapi.LocatedSupplier, 0, len(rows))}
	resp.About.Count = int64(len(rows))
	for _, row := range rows {
		resp.Items = append(resp.Items, toApiLocatedSupplier(r, row))
	}
	WriteJsonResponse(w, resp)
}

// AddLocatedSupplier adds a symbol without selecting or sending to it.
func (a *ApiHandler) AddLocatedSupplier(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{TransactionId: id})
	owner := a.rotaTenant(ctx, w, r)
	if owner == nil {
		return
	}
	var body oapi.AddLocatedSupplier
	if err := decodeRotaBody(w, r, &body); err != nil {
		AddBadRequestError(ctx, w, err)
		return
	}
	authority, symbol, ok := strings.Cut(body.SupplierSymbol, ":")
	if !ok || strings.TrimSpace(authority) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(body.SupplierSymbol) != body.SupplierSymbol || len(body.SupplierSymbol) > 255 || strings.TrimSpace(body.LocalId) == "" || len(body.LocalId) > 1024 {
		AddBadRequestError(ctx, w, errors.New("supplierSymbol must include an authority and localId must be nonempty (maximum lengths 255 and 1024)"))
		return
	}
	row, err := a.rotaService.Add(ctx, id, body.SupplierSymbol, body.LocalId, owner)
	if err != nil {
		writeRotaError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiLocatedSupplier(r, row))
}

func decodeRotaBody(w http.ResponseWriter, r *http.Request, body any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(body); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected a single JSON object")
	}
	return nil
}

func writeRotaError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrRotaNotFound):
		AddNotFoundError(w)
	case errors.Is(err, service.ErrRotaConflict):
		WriteJsonErrorResponse(w, err, http.StatusConflict)
	case errors.Is(err, service.ErrRotaSymbol):
		WriteJsonErrorResponse(w, err, http.StatusUnprocessableEntity)
	default:
		AddInternalError(ctx, w, err)
	}
}
