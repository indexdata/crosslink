package prapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
)

// PrefillProvider supplies catalog metadata for the authorized requester.
type PrefillProvider interface {
	Prefill(common.ExtendedContext, string, catalog.LookupParams) (prservice.Prefill, error)
}

// SetPrefillProvider wires the read-only catalog service.
func (a *PatronRequestApiHandler) SetPrefillProvider(provider PrefillProvider) {
	a.prefillProvider = provider
}

var prefillISBN = regexp.MustCompile(`^(?:[0-9]{9}[0-9Xx]|[0-9]{13})$`)
var prefillISSN = regexp.MustCompile(`^[0-9]{7}[0-9Xx]$`)

func validatePrefill(params catalog.LookupParams) error {
	if params.Identifier == "" && params.Isbn == "" && params.Issn == "" {
		return errors.New("at least one of identifier, isbn, or issn is required")
	}
	for _, value := range []string{params.Identifier, params.Isbn, params.Issn} {
		if len(value) > 1024 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("invalid lookup parameter")
		}
	}
	compact := func(s string) string { return strings.NewReplacer("-", "", " ", "").Replace(s) }
	if params.Isbn != "" && !prefillISBN.MatchString(compact(params.Isbn)) {
		return errors.New("isbn must be ISBN-10 or ISBN-13")
	}
	if params.Issn != "" && !prefillISSN.MatchString(compact(params.Issn)) {
		return errors.New("issn must contain seven digits and a check digit or X")
	}
	switch params.ServiceType {
	case "", "Loan", "Copy", "CopyOrLoan":
	default:
		return errors.New("serviceType must be Loan, Copy, or CopyOrLoan")
	}
	return nil
}

func (a *PatronRequestApiHandler) GetPatronRequestsPrefill(w http.ResponseWriter, r *http.Request, params proapi.GetPatronRequestsPrefillParams) {
	deadline, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ctx := common.CreateExtCtxWithArgs(deadline, &common.LoggerArgs{Other: map[string]string{"method": "GetPatronRequestsPrefill"}})
	value := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	lookup := catalog.LookupParams{Identifier: value(params.Identifier), Isbn: value(params.Isbn), Issn: value(params.Issn)}
	if params.ServiceType != nil {
		lookup.ServiceType = string(*params.ServiceType)
	}
	if err := validatePrefill(lookup); err != nil {
		api.WriteJsonErrorResponse(w, err, http.StatusBadRequest)
		return
	}
	symbol, err := a.getRequestSymbol(ctx, r, params.Symbol)
	if err != nil {
		api.WriteJsonErrorResponse(w, errors.New("invalid requester tenant or symbol"), http.StatusBadRequest)
		return
	}
	if symbol == "" {
		api.WriteJsonErrorResponse(w, errors.New("symbol is required for unprefixed requests"), http.StatusBadRequest)
		return
	}
	if a.prefillProvider == nil {
		api.WriteJsonErrorResponse(w, prservice.ErrPrefillUnavailable, http.StatusServiceUnavailable)
		return
	}
	result, err := a.prefillProvider.Prefill(ctx, symbol, lookup)
	if err != nil {
		status := http.StatusBadGateway
		publicError := errors.New("catalog lookup failed")
		switch {
		case errors.Is(err, catalog.ErrMissingLookupParameters):
			status = http.StatusBadRequest
			publicError = catalog.ErrMissingLookupParameters
		case errors.Is(err, prservice.ErrPrefillNotFound):
			status = http.StatusNotFound
			publicError = prservice.ErrPrefillNotFound
		case errors.Is(err, prservice.ErrPrefillUnavailable):
			status = http.StatusServiceUnavailable
			publicError = prservice.ErrPrefillUnavailable
		}
		api.WriteJsonErrorResponse(w, publicError, status)
		return
	}
	api.WriteJsonResponse(w, result)
}
