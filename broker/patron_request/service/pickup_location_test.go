package prservice

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
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
		{name: "missing shipping address", id: "11111111-1111-4111-8111-111111111111", entries: []dirapi.Entry{{Id: branch.Id, Name: branch.Name, LmsConfig: branch.LmsConfig}}, wantError: true},
		{name: "send to patron", id: "11111111-1111-4111-8111-111111111111", direct: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pickupRepo := new(IllRepoMock)
			if tc.id != "" && !tc.direct {
				peer := ill_db.Peer{}
				var lookupErr error
				if len(tc.entries) > 0 {
					peer.CustomData = tc.entries[0]
				} else {
					lookupErr = errors.New("directory entry not found")
				}
				pickupRepo.On("GetCachedPeerByDirectoryEntryID", uuid.MustParse(tc.id), mock.Anything).Return(peer, "<cached>", lookupErr)
			}
			service := PatronRequestActionService{illRepo: pickupRepo}
			t.Cleanup(func() { pickupRepo.AssertExpectations(t) })
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
				pickupRepo.AssertNotCalled(t, "GetCachedPeerByDirectoryEntryID", mock.Anything, mock.Anything)
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

func TestPickupLocationTenantValidation(t *testing.T) {
	for _, query := range []string{"<cached>", "/by-id/entry"} {
		t.Run(query, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				requestTenant pgtype.Text
				entryTenant   *string
				allowed       bool
			}{
				{name: "same tenant", requestTenant: pgtype.Text{String: "tenant-a", Valid: true}, entryTenant: new("tenant-a"), allowed: true},
				{name: "different tenant", requestTenant: pgtype.Text{String: "tenant-a", Valid: true}, entryTenant: new("tenant-b")},
				{name: "missing entry tenant", requestTenant: pgtype.Text{String: "tenant-a", Valid: true}},
				{name: "empty entry tenant", requestTenant: pgtype.Text{String: "tenant-a", Valid: true}, entryTenant: new("")},
				{name: "normalized request tenant", requestTenant: pgtype.Text{String: " tenant-a ", Valid: true}, entryTenant: new("tenant-a"), allowed: true},
				{name: "standalone request", allowed: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					id := uuid.New()
					entry := dirapi.Entry{Id: &id, Name: "Branch", Tenant: tc.entryTenant}
					pickupRepo := new(IllRepoMock)
					pickupRepo.On("GetCachedPeerByDirectoryEntryID", id, mock.Anything).Return(ill_db.Peer{CustomData: entry}, query, nil).Once()
					service := PatronRequestActionService{illRepo: pickupRepo}
					result, err := service.pickupLocationEntry(appCtx, pr_db.PatronRequest{
						Tenant:                    tc.requestTenant,
						RequesterPickupLocationID: pgtype.UUID{Bytes: id, Valid: true},
					})
					if tc.allowed {
						require.NoError(t, err)
						require.Equal(t, entry, result)
					} else {
						require.ErrorContains(t, err, "does not belong to request tenant")
						require.Equal(t, dirapi.Entry{}, result)
					}
					pickupRepo.AssertExpectations(t)
				})
			}
		})
	}
}

func TestPickupLocationInheritedTenant(t *testing.T) {
	childID, parentID, grandparentID := uuid.New(), uuid.New(), uuid.New()
	for _, tc := range []struct {
		name        string
		childTenant *string
		parents     []dirapi.Entry
		lookupError error
		allowed     bool
	}{
		{name: "parent tenant", parents: []dirapi.Entry{{Id: &parentID, Tenant: new("tenant-a")}}, allowed: true},
		{name: "empty child tenant inherits", childTenant: new(""), parents: []dirapi.Entry{{Id: &parentID, Tenant: new("tenant-a")}}, allowed: true},
		{name: "grandparent tenant", parents: []dirapi.Entry{{Id: &parentID, Parent: &grandparentID}, {Id: &grandparentID, Tenant: new("tenant-a")}}, allowed: true},
		{name: "different parent tenant", parents: []dirapi.Entry{{Id: &parentID, Tenant: new("tenant-b")}}},
		{name: "explicit child mismatch stops inheritance", childTenant: new("tenant-b")},
		{name: "explicit child match needs no parent", childTenant: new("tenant-a"), allowed: true},
		{name: "nearest parent mismatch stops inheritance", parents: []dirapi.Entry{{Id: &parentID, Parent: &grandparentID, Tenant: new("tenant-b")}}},
		{name: "no tenant on ancestors", parents: []dirapi.Entry{{Id: &parentID}}},
		{name: "missing parent", lookupError: errors.New("parent not found"), parents: []dirapi.Entry{{Id: &parentID}}},
		{name: "parent cycle", parents: []dirapi.Entry{{Id: &parentID, Parent: &childID}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := dirapi.Entry{Id: &childID, Name: "Selected branch", Parent: &parentID, Tenant: tc.childTenant,
				LmsConfig: &dirapi.LmsConfig{RequesterPickupLocation: new("branch-1")},
			}
			pickupRepo := new(IllRepoMock)
			pickupRepo.On("GetCachedPeerByDirectoryEntryID", childID, mock.Anything).Return(ill_db.Peer{CustomData: child}, "<cached>", nil).Once()
			for _, parent := range tc.parents {
				pickupRepo.On("GetCachedPeerByDirectoryEntryID", *parent.Id, mock.Anything).Return(ill_db.Peer{CustomData: parent}, "<cached>", tc.lookupError).Once()
			}
			service := PatronRequestActionService{illRepo: pickupRepo}
			result, err := service.pickupLocationEntry(appCtx, pr_db.PatronRequest{
				Tenant:                    pgtype.Text{String: "tenant-a", Valid: true},
				RequesterPickupLocationID: pgtype.UUID{Bytes: childID, Valid: true},
			})
			if tc.allowed {
				require.NoError(t, err)
				// Tenant inheritance must not replace the selected branch's pickup data.
				require.Equal(t, child, result)
			} else {
				require.Error(t, err)
				require.Equal(t, dirapi.Entry{}, result)
			}
			pickupRepo.AssertExpectations(t)
		})
	}
}

func TestFillLocallyRejectsInvalidPickupSelection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tenant      string
		config      *dirapi.LmsConfig
		lookupError error
	}{
		{name: "lookup fails", lookupError: errors.New("directory unavailable")},
		{name: "missing pickup code", tenant: "tenant-a"},
		{name: "empty pickup code", tenant: "tenant-a", config: &dirapi.LmsConfig{RequesterPickupLocation: new("")}},
		{name: "wrong tenant", tenant: "tenant-b", config: &dirapi.LmsConfig{RequesterPickupLocation: new("branch")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			pickupRepo := new(IllRepoMock)
			pickupRepo.On("GetCachedPeerByDirectoryEntryID", id, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{
				Id: &id, Tenant: &tc.tenant, LmsConfig: tc.config,
			}}, "<cached>", tc.lookupError).Once()
			service := PatronRequestActionService{illRepo: pickupRepo}
			lmsAdapter := &mockLmsAdapter{requesterPickupLocation: "default"}
			pr := pr_db.PatronRequest{Tenant: pgtype.Text{String: "tenant-a", Valid: true}, RequesterPickupLocationID: pgtype.UUID{Bytes: id, Valid: true}}
			result := service.fillLocallyBorrowingRequest(appCtx, "", pr, lmsAdapter, iso18626.Request{}, actionParams{})
			require.Equal(t, events.EventStatusError, result.status)
			lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			pickupRepo.AssertExpectations(t)
		})
	}
}
