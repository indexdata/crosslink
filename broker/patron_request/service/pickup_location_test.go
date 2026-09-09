package prservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestPickupLocationAddress(t *testing.T) {
	var branch dirapi.Entry
	require.NoError(t, json.Unmarshal([]byte(`{"name":"Branch","lmsConfig":{"requesterPickupLocation":"branch-1"},"addresses":[{"type":"Shipping","addressComponents":[{"type":"Thoroughfare","value":"Branch Street 1"},{"type":"PostalCode","value":"12345"}]}]}`), &branch))
	for _, tc := range []struct {
		name, code        string
		entries           []dirapi.Entry
		wantError, direct bool
	}{
		{name: "branch without symbols", code: "branch-1", entries: []dirapi.Entry{branch}},
		{name: "not selected"},
		{name: "unknown", code: "missing", entries: []dirapi.Entry{branch}, wantError: true},
		{name: "ambiguous", code: "branch-1", entries: []dirapi.Entry{branch, branch}, wantError: true},
		{name: "missing shipping address", code: "branch-1", entries: []dirapi.Entry{{Name: branch.Name, LmsConfig: branch.LmsConfig}}, wantError: true},
		{name: "send to patron", code: "branch-1", direct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, `(symbol any "ISIL:MAIN" or parentSymbol any "ISIL:MAIN") and requesterPickupLocation="`+tc.code+`"`, r.URL.Query().Get("cql"))
				require.NoError(t, json.NewEncoder(w).Encode(dirapi.EntriesResponse{Items: tc.entries}))
			}))
			defer server.Close()
			service := PatronRequestActionService{directoryLookupAdapter: adapter.CreateApiDirectory(server.Client(), []string{server.URL})}
			pr := pr_db.PatronRequest{RequesterSymbol: pgtype.Text{String: "ISIL:MAIN", Valid: true}, RequesterPickupLocation: pgtype.Text{String: tc.code, Valid: true}}
			request := iso18626.Request{Header: iso18626.Header{RequestingAgencyId: iso18626.TypeAgencyId{AgencyIdValue: "MAIN"}}, RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{{Address: &iso18626.Address{PhysicalAddress: &iso18626.PhysicalAddress{Line1: "Original address"}}}}}
			if tc.direct {
				yes := iso18626.TypeYesNoY
				request.PatronInfo = &iso18626.PatronInfo{SendToPatron: &yes}
			}
			result, err := service.applyPickupLocationAddress(appCtx, pr, request)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, request.Header, result.Header)
			require.Equal(t, "Original address", request.RequestedDeliveryInfo[0].Address.PhysicalAddress.Line1)
			if tc.code == "" || tc.direct {
				require.Zero(t, calls)
				require.Equal(t, request, result)
			} else {
				require.Equal(t, "Branch Street 1", result.RequestedDeliveryInfo[0].Address.PhysicalAddress.Line1)
				require.Equal(t, "12345", result.RequestedDeliveryInfo[0].Address.PhysicalAddress.PostalCode)
				request.RequestedDeliveryInfo = []iso18626.RequestedDeliveryInfo{{Address: &iso18626.Address{ElectronicAddress: &iso18626.ElectronicAddress{ElectronicAddressData: "library@example.org"}}}}
				result, err = service.applyPickupLocationAddress(appCtx, pr, request)
				require.NoError(t, err)
				require.Len(t, result.RequestedDeliveryInfo, 2)
				require.Equal(t, "library@example.org", result.RequestedDeliveryInfo[0].Address.ElectronicAddress.ElectronicAddressData)
				require.Equal(t, "Branch Street 1", result.RequestedDeliveryInfo[1].Address.PhysicalAddress.Line1)

			}
		})
	}
}
