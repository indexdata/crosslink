package prservice

import (
	"encoding/json"
	"github.com/google/uuid"
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
	require.NoError(t, json.Unmarshal([]byte(`{"id":"11111111-1111-4111-8111-111111111111","name":"Branch","lmsConfig":{"requesterPickupLocation":"branch-1"},"addresses":[{"type":"Shipping","addressComponents":[{"type":"Thoroughfare","value":"Branch Street 1"},{"type":"PostalCode","value":"12345"}]}]}`), &branch))
	for _, tc := range []struct {
		name, id          string
		entries           []dirapi.Entry
		wantError, direct bool
	}{
		{name: "branch without symbols", id: "11111111-1111-4111-8111-111111111111", entries: []dirapi.Entry{branch}},
		{name: "not selected"},
		{name: "not found", id: "22222222-2222-4222-8222-222222222222", wantError: true},
		{name: "wrong entry returned", id: "22222222-2222-4222-8222-222222222222", entries: []dirapi.Entry{branch}, wantError: true},
		{name: "missing shipping address", id: "11111111-1111-4111-8111-111111111111", entries: []dirapi.Entry{{Id: branch.Id, Name: branch.Name, LmsConfig: branch.LmsConfig}}, wantError: true},
		{name: "send to patron", id: "11111111-1111-4111-8111-111111111111", direct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "/by-id/"+tc.id, r.URL.Path)
				require.Empty(t, r.URL.RawQuery)
				if len(tc.entries) == 0 {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				require.NoError(t, json.NewEncoder(w).Encode(tc.entries[0]))
			}))
			defer server.Close()
			service := PatronRequestActionService{directoryLookupAdapter: adapter.CreateApiDirectory(server.Client(), []string{server.URL})}
			pr := pr_db.PatronRequest{RequesterSymbol: pgtype.Text{String: "ISIL:MAIN", Valid: true}, RequesterPickupLocationID: pickupID(tc.id)}
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
			if tc.id == "" || tc.direct {
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

func pickupID(id string) pgtype.UUID {
	if id == "" {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: uuid.MustParse(id), Valid: true}
}

func TestPickupLocationLookupAcrossDirectories(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	found := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/by-id/"+id, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"11111111-1111-4111-8111-111111111111","name":"Selected branch","lmsConfig":{"requesterPickupLocation":"shared-code"}}`))
	}))
	defer found.Close()
	service := PatronRequestActionService{directoryLookupAdapter: adapter.CreateApiDirectory(found.Client(), []string{missing.URL, found.URL})}
	entry, err := service.pickupLocationEntry(appCtx, pr_db.PatronRequest{RequesterPickupLocationID: pickupID(id)})
	require.NoError(t, err)
	require.Equal(t, "Selected branch", entry.Name)
	require.Equal(t, "shared-code", *entry.LmsConfig.RequesterPickupLocation)
}
