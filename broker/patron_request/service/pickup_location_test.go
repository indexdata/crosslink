package prservice

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/lms"
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
					parentID := uuid.New()
					peer.CustomData.Parent = &parentID
					pickupRepo.On("GetCachedPeerByDirectoryEntryID", parentID, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{Symbols: pickupSymbols("MAIN")}}, "<cached>", nil)
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

func pickupSymbols(symbol string) *[]dirapi.Symbol {
	return &[]dirapi.Symbol{{Authority: "ISIL", Symbol: symbol}}
}

func TestPickupLocationInstitutionValidation(t *testing.T) {
	childID, parentID, grandparentID := uuid.New(), uuid.New(), uuid.New()
	for _, query := range []string{"<cached>", "/by-id/entry"} {
		for _, tc := range []struct {
			name        string
			parents     []dirapi.Entry
			lookupError error
			allowed     bool
		}{
			{name: "parent institution", parents: []dirapi.Entry{{Id: &parentID, Symbols: pickupSymbols("MAIN")}}, allowed: true},
			{name: "grandparent institution", parents: []dirapi.Entry{{Id: &parentID, Parent: &grandparentID, Symbols: pickupSymbols("BRANCH")}, {Id: &grandparentID, Symbols: pickupSymbols("MAIN")}}, allowed: true},
			{name: "different tenant same institution", parents: []dirapi.Entry{{Id: &parentID, Tenant: new("tenant-b"), Symbols: pickupSymbols("MAIN")}}, allowed: true},
			{name: "same tenant different institution", parents: []dirapi.Entry{{Id: &parentID, Tenant: new("tenant-a"), Symbols: pickupSymbols("OTHER")}}},
			{name: "default authority", parents: []dirapi.Entry{{Id: &parentID, Symbols: &[]dirapi.Symbol{{Symbol: "MAIN"}}}}, allowed: true},
			{name: "wrong authority", parents: []dirapi.Entry{{Id: &parentID, Symbols: &[]dirapi.Symbol{{Authority: "OTHER", Symbol: "MAIN"}}}}},
			{name: "no ancestor symbols", parents: []dirapi.Entry{{Id: &parentID}}},
			{name: "missing parent", lookupError: errors.New("parent not found"), parents: []dirapi.Entry{{Id: &parentID}}},
			{name: "parent cycle", parents: []dirapi.Entry{{Id: &parentID, Parent: &childID}}},
			{name: "not a branch"},
		} {
			t.Run(query+"/"+tc.name, func(t *testing.T) {
				child := dirapi.Entry{Id: &childID, Name: "Selected branch", Tenant: new("tenant-a"),
					LmsConfig: &dirapi.LmsConfig{RequesterPickupLocation: new("branch-1")}}
				if len(tc.parents) > 0 {
					child.Parent = &parentID
				}
				pickupRepo := new(IllRepoMock)
				pickupRepo.On("GetCachedPeerByDirectoryEntryID", childID, mock.Anything).Return(ill_db.Peer{CustomData: child}, query, nil).Once()
				for _, parent := range tc.parents {
					pickupRepo.On("GetCachedPeerByDirectoryEntryID", *parent.Id, mock.Anything).Return(ill_db.Peer{CustomData: parent}, query, tc.lookupError).Once()
				}
				service := PatronRequestActionService{illRepo: pickupRepo}
				result, err := service.pickupLocationEntry(appCtx, pr_db.PatronRequest{
					RequesterSymbol:           pgtype.Text{String: " ISIL:MAIN ", Valid: true},
					Tenant:                    pgtype.Text{String: "tenant-a", Valid: true},
					RequesterPickupLocationID: pgtype.UUID{Bytes: childID, Valid: true},
				})
				if tc.allowed {
					require.NoError(t, err)
					require.Equal(t, child, result)
				} else {
					require.Error(t, err)
					require.Equal(t, dirapi.Entry{}, result)
				}
				pickupRepo.AssertExpectations(t)
			})
		}
	}
}

func TestPickupLocationRequiresRequesterSymbol(t *testing.T) {
	for _, symbol := range []pgtype.Text{{}, {String: "ISIL:MAIN"}, {Valid: true}, {String: "  ", Valid: true}} {
		service := PatronRequestActionService{}
		_, err := service.pickupLocationEntry(appCtx, pr_db.PatronRequest{RequesterSymbol: symbol})
		require.ErrorContains(t, err, "requires a requester symbol")
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
		{name: "wrong institution", tenant: "tenant-b", config: &dirapi.LmsConfig{RequesterPickupLocation: new("branch")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := uuid.New()
			pickupRepo := new(IllRepoMock)
			parentID := uuid.New()
			parentSymbol := "MAIN"
			if tc.tenant == "tenant-b" {
				parentSymbol = "OTHER"
			}
			if tc.lookupError == nil {
				pickupRepo.On("GetCachedPeerByDirectoryEntryID", parentID, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{Symbols: pickupSymbols(parentSymbol)}}, "<cached>", nil).Once()
			}
			pickupRepo.On("GetCachedPeerByDirectoryEntryID", id, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{
				Id: &id, Parent: &parentID, Tenant: &tc.tenant, LmsConfig: tc.config,
			}}, "<cached>", tc.lookupError).Once()
			service := PatronRequestActionService{illRepo: pickupRepo}
			lmsAdapter := &mockLmsAdapter{requesterPickupLocation: "default"}
			pr := pr_db.PatronRequest{RequesterSymbol: pgtype.Text{String: "ISIL:MAIN", Valid: true}, Tenant: pgtype.Text{String: "tenant-a", Valid: true}, RequesterPickupLocationID: pgtype.UUID{Bytes: id, Valid: true}}
			result := service.fillLocallyBorrowingRequest(appCtx, "", pr, lmsAdapter, iso18626.Request{}, actionParams{})
			require.Equal(t, events.EventStatusError, result.status)
			lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			pickupRepo.AssertExpectations(t)
		})
	}
}

func TestPickupCodeRequirementPerOperation(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		config                          dirapi.LmsConfig
		requestUsesCode, acceptUsesCode bool
	}{
		{name: "default", requestUsesCode: true, acceptUsesCode: true},
		{name: "RequestItem disabled", config: dirapi.LmsConfig{RequestItemEnabled: new(false)}, acceptUsesCode: true},
		{name: "RequestItem pickup field disabled", config: dirapi.LmsConfig{RequestItemPickupLocationEnabled: new(false)}, acceptUsesCode: true},
		{name: "AcceptItem disabled", config: dirapi.LmsConfig{AcceptItemEnabled: new(false)}, requestUsesCode: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.config.Address = "http://unused.invalid"
			tc.config.FromAgency = "MAIN"
			adapter, err := lms.CreateLmsAdapterNcip(tc.config)
			require.NoError(t, err)
			require.Equal(t, tc.requestUsesCode, adapter.RequestItemUsesPickupLocation())
			require.Equal(t, tc.acceptUsesCode, adapter.AcceptItemUsesPickupLocation())
			id, parentID := uuid.New(), uuid.New()
			repo := new(IllRepoMock)
			lookups := 0
			if tc.requestUsesCode {
				lookups++
			}
			if tc.acceptUsesCode {
				lookups++
			}
			repo.On("GetCachedPeerByDirectoryEntryID", id, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{Id: &id, Parent: &parentID}}, "<cached>", nil).Times(lookups)
			repo.On("GetCachedPeerByDirectoryEntryID", parentID, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{Symbols: pickupSymbols("MAIN")}}, "<cached>", nil).Times(lookups)
			service := PatronRequestActionService{illRepo: repo}
			pr := pr_db.PatronRequest{RequesterSymbol: pgtype.Text{String: "ISIL:MAIN", Valid: true}, RequesterPickupLocationID: pgtype.UUID{Bytes: id, Valid: true}}
			for _, usesCode := range []bool{adapter.RequestItemUsesPickupLocation(), adapter.AcceptItemUsesPickupLocation()} {
				code, err := service.requesterPickupCode(appCtx, pr, adapter, usesCode)
				if usesCode {
					require.ErrorContains(t, err, "has no LMS pickup location code")
				} else {
					require.NoError(t, err)
					require.Empty(t, code)
				}
			}
			repo.AssertExpectations(t)
		})
	}
}

func TestPickupLocationWithMockDirectory(t *testing.T) {
	for _, configured := range []string{"", "ISIL:REQUESTER"} {
		t.Run("institution="+configured, func(t *testing.T) {
			t.Setenv("MOCK_PICKUP_INSTITUTION_SYMBOL", configured)
			ownerSymbol := configured
			if ownerSymbol == "" {
				ownerSymbol = "ISIL:MOCK"
			}
			directory := &adapter.MockDirectoryLookupAdapter{}
			id := uuid.New()
			locations, _, err := directory.Lookup(appCtx, adapter.DirectoryLookupParams{EntryID: id.String()})
			require.NoError(t, err)
			entry := locations[0].CustomData
			require.NotNil(t, entry.Parent)
			parents, _, err := directory.Lookup(appCtx, adapter.DirectoryLookupParams{EntryID: entry.Parent.String()})
			require.NoError(t, err)
			repo := new(IllRepoMock)
			repo.On("GetCachedPeerByDirectoryEntryID", id, mock.Anything).Return(ill_db.Peer{CustomData: entry}, "<cached>", nil).Twice()
			repo.On("GetCachedPeerByDirectoryEntryID", *entry.Parent, mock.Anything).Return(ill_db.Peer{CustomData: parents[0].CustomData}, "<cached>", nil).Twice()
			service := PatronRequestActionService{illRepo: repo, directoryLookupAdapter: directory}
			pr := pr_db.PatronRequest{RequesterSymbol: pgtype.Text{String: ownerSymbol, Valid: true}, RequesterPickupLocationID: pgtype.UUID{Bytes: id, Valid: true}}
			code, err := service.requesterPickupCode(appCtx, pr, &lms.LmsAdapterManual{}, true)
			require.NoError(t, err)
			require.Equal(t, id.String(), code)
			pr.RequesterSymbol.String = "ISIL:UNRELATED"
			_, err = service.pickupLocationEntry(appCtx, pr)
			require.ErrorContains(t, err, "is not a branch of requester institution")
			repo.AssertExpectations(t)
		})
	}
}

func TestUnusedPickupCodeSkipsLookup(t *testing.T) {
	for _, selected := range []bool{false, true} {
		repo := new(IllRepoMock)
		service := PatronRequestActionService{illRepo: repo}
		pr := pr_db.PatronRequest{RequesterPickupLocationID: pgtype.UUID{Bytes: uuid.New(), Valid: selected}}
		// No adapter or requester symbol is needed when pickup data is unused.
		code, err := service.requesterPickupCode(appCtx, pr, nil, false)
		require.NoError(t, err)
		require.Empty(t, code)
		repo.AssertNotCalled(t, "GetCachedPeerByDirectoryEntryID", mock.Anything, mock.Anything)
	}
}

func TestValidateRequesterPickupLocationWithoutOperationData(t *testing.T) {
	id, parentID := uuid.New(), uuid.New()
	repo := new(IllRepoMock)
	repo.On("GetCachedPeerByDirectoryEntryID", id, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{Id: &id, Parent: &parentID}}, "<cached>", nil).Once()
	repo.On("GetCachedPeerByDirectoryEntryID", parentID, mock.Anything).Return(ill_db.Peer{CustomData: dirapi.Entry{Symbols: pickupSymbols("MAIN")}}, "<cached>", nil).Once()
	service := PatronRequestActionService{illRepo: repo}
	require.NoError(t, service.ValidateRequesterPickupLocation(appCtx, pr_db.PatronRequest{}))
	require.NoError(t, service.ValidateRequesterPickupLocation(appCtx, pr_db.PatronRequest{
		RequesterSymbol:           pgtype.Text{String: "ISIL:MAIN", Valid: true},
		RequesterPickupLocationID: pgtype.UUID{Bytes: id, Valid: true},
	}))
	repo.AssertExpectations(t)
}
