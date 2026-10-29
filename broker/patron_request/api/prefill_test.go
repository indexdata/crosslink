package prapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type prefillFunc func(common.ExtendedContext, string, catalog.LookupParams) (prservice.Prefill, error)

func (f prefillFunc) Prefill(ctx common.ExtendedContext, symbol string, params catalog.LookupParams) (prservice.Prefill, error) {
	return f(ctx, symbol, params)
}

func TestPrefillRoutes(t *testing.T) {
	for _, prefix := range []string{"", "/broker"} {
		for _, query := range []string{"identifier=record", "isbn=978-3-16-148410-0", "issn=1234-567X", "identifier=record&isbn=9783161484100&issn=1234567X"} {
			for _, serviceType := range []string{"", "Loan", "Copy", "CopyOrLoan"} {
				t.Run(prefix+query+serviceType, func(t *testing.T) {
					handler := &PatronRequestApiHandler{tenantResolver: tenant.NewResolver().WithTenantToSymbol("ISIL:{tenant}")}
					called := false
					handler.SetPrefillProvider(prefillFunc(func(ctx common.ExtendedContext, symbol string, params catalog.LookupParams) (prservice.Prefill, error) {
						called = true
						require.Equal(t, "ISIL:REQ", symbol)
						require.Equal(t, serviceType, params.ServiceType)
						_, bounded := ctx.Deadline()
						require.True(t, bounded)
						result := prservice.Prefill{BibliographicInfo: &iso18626.BibliographicInfo{Title: "Title"}}
						if serviceType != "" {
							result.ServiceInfo = &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceType(serviceType)}
						}
						return result, nil
					}))
					mux := http.NewServeMux()
					proapi.HandlerFromMuxWithBaseURL(handler, mux, prefix)
					target := prefix + "/patron_requests/prefill?" + query
					if prefix == "" {
						target += "&symbol=ISIL:REQ"
					}
					if serviceType != "" {
						target += "&serviceType=" + serviceType
					}
					req := httptest.NewRequest(http.MethodGet, target, nil)
					req.Header.Set("X-Okapi-Tenant", "req")
					response := httptest.NewRecorder()
					mux.ServeHTTP(response, req)
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.True(t, called)
					if serviceType == "" {
						assert.NotContains(t, response.Body.String(), "serviceInfo")
					} else {
						assert.Contains(t, response.Body.String(), `"serviceType":"`+serviceType+`"`)
					}
				})
			}
		}
	}
}

func TestPrefillInvalidParametersAndTenant(t *testing.T) {
	for _, target := range []string{
		"/patron_requests/prefill?symbol=ISIL:REQ", "/patron_requests/prefill?identifier=%20&symbol=ISIL:REQ",
		"/patron_requests/prefill?serviceType=Loan&symbol=ISIL:REQ", "/patron_requests/prefill?identifier=record",
		"/patron_requests/prefill?isbn=bad&symbol=ISIL:REQ", "/patron_requests/prefill?issn=bad&symbol=ISIL:REQ",
		"/patron_requests/prefill?identifier=record&serviceType=Invalid&symbol=ISIL:REQ",
		"/broker/patron_requests/prefill?identifier=record&symbol=ISIL:OTHER",
	} {
		t.Run(target, func(t *testing.T) {
			handler := &PatronRequestApiHandler{tenantResolver: tenant.NewResolver().WithTenantToSymbol("ISIL:{tenant}")}
			handler.SetPrefillProvider(prefillFunc(func(common.ExtendedContext, string, catalog.LookupParams) (prservice.Prefill, error) {
				t.Fatal("invalid request reached catalog")
				return prservice.Prefill{}, nil
			}))
			mux := http.NewServeMux()
			proapi.HandlerFromMux(handler, mux)
			proapi.HandlerFromMuxWithBaseURL(handler, mux, "/broker")
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("X-Okapi-Tenant", "req")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}

func TestPrefillErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{catalog.ErrMissingLookupParameters, 400}, {prservice.ErrPrefillNotFound, 404}, {prservice.ErrPrefillUnavailable, 503}, {errors.New("secret upstream credentials"), 502},
	} {
		handler := &PatronRequestApiHandler{tenantResolver: tenant.NewResolver()}
		handler.SetPrefillProvider(prefillFunc(func(common.ExtendedContext, string, catalog.LookupParams) (prservice.Prefill, error) {
			return prservice.Prefill{}, tc.err
		}))
		mux := http.NewServeMux()
		proapi.HandlerFromMux(handler, mux)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/patron_requests/prefill?identifier=id&symbol=ISIL:REQ", nil))
		assert.Equal(t, tc.status, response.Code)
		assert.NotContains(t, response.Body.String(), "secret")
	}
}
