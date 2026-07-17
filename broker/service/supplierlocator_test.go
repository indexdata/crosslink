package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)

func TestGetDateWithTimezone(t *testing.T) {
	act, err := time.LoadLocation("Australia/ACT")
	assert.NoError(t, err)

	date, err := getDateWithTimezone("2026-12-16", act, false)
	assert.NoError(t, err)
	assert.Equal(t, 2026, date.UTC().Year())
	assert.Equal(t, time.December, date.UTC().Month())
	assert.Equal(t, 15, date.UTC().Day())
	assert.Equal(t, "Australia/ACT", date.Location().String())
	assert.Equal(t, 13, date.UTC().Hour())
	assert.Equal(t, 0, date.UTC().Minute())
	assert.Equal(t, "2026-12-15 13:00:00 +0000 UTC", date.UTC().String())

	date, err = getDateWithTimezone("2026-12-16", act, true)
	assert.NoError(t, err)
	assert.Equal(t, 2026, date.Year())
	assert.Equal(t, time.December, date.Month())
	assert.Equal(t, 16, date.Day())
	assert.Equal(t, "Australia/ACT", date.Location().String())
	assert.Equal(t, 12, date.UTC().Hour())
	assert.Equal(t, 59, date.UTC().Minute())
	assert.Equal(t, "2026-12-16 12:59:59.999999999 +0000 UTC", date.UTC().String())

	_, err = time.LoadLocation("Unexpected")
	assert.Error(t, err)

	_, err = getDateWithTimezone("2026-12-36", act, true)
	assert.Equal(t, "parsing time \"2026-12-36\": day out of range", err.Error())

	auto := time.Now().Location()
	date, err = getDateWithTimezone("2026-12-16", auto, true)
	assert.NoError(t, err)
	assert.Equal(t, 2026, date.Year())
	assert.Equal(t, time.December, date.Month())
	assert.Equal(t, 16, date.Day())
	assert.Equal(t, "Local", date.Location().String())
}

func TestGetNextSupplierEmptyMap(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "1", locSup.ID)
}

func TestGetNextSupplierClosed(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	past := time.Now().Add(-48 * time.Hour).Format(DATE_LAYOUT)
	future := time.Now().Add(48 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + past + "\"," +
		"\"endDate\": \"" + future + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId, SupplierSymbol: "ISIL:SUP"}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 1)
	assert.Equal(t, "", locSup.ID)
	assert.Equal(t, "ISIL:SUP", skipped[0].Symbol)
	assert.True(t, strings.Contains(skipped[0].Reason, "closed on"))
}

func TestGetNextSupplierFailToLoadPeer(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{}, errors.New("db error"))
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.Equal(t, "db error", err.Error())
	assert.Len(t, skipped, 0)
	assert.Equal(t, "", locSup.ID)
}

func TestGetNextSupplierNoClosures(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	jsonData := "{" +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "1", locSup.ID)
}

func TestGetNextSupplierNoStartDate(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	future := time.Now().Add(48 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"endDate\": \"" + future + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "1", locSup.ID)
}

func TestGetNextSupplierNoEndDate(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	past := time.Now().Add(-48 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + past + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "1", locSup.ID)
}

func TestGetNextSupplierBothInPast(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	past := time.Now().Add(-48 * time.Hour).Format(DATE_LAYOUT)
	end := time.Now().Add(-24 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + past + "\"," +
		"\"endDate\": \"" + end + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "1", locSup.ID)
}

func TestGetNextSupplierBothInFuture(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	start := time.Now().Add(48 * time.Hour).Format(DATE_LAYOUT)
	end := time.Now().Add(72 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + start + "\"," +
		"\"endDate\": \"" + end + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "1", locSup.ID)
}

func TestGetNextSupplierCannotParseDate(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	end := time.Now().Add(72 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"2025-12-35\"," +
		"\"endDate\": \"" + end + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.Error(t, err)
	return
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 1)
	assert.Equal(t, "", locSup.ID)
}

func TestGetNextSupplierCannotParseEndDate(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	start := time.Now().Add(-48 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + start + "\"," +
		"\"endDate\": \"2025-12-35\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.Error(t, err)
	return
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 1)
	assert.Equal(t, "", locSup.ID)
}

func TestGetNextSupplierBetweenHolidays(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	timezoneLoc, err := time.LoadLocation("Australia/Victoria")
	assert.NoError(t, err)
	yesterday := time.Now().Add(-24 * time.Hour).In(timezoneLoc).Format(DATE_LAYOUT)
	tomorrow := time.Now().Add(24 * time.Hour).In(timezoneLoc).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + yesterday + "\"," +
		"\"endDate\": \"" + yesterday + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}," +
		"{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda363\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb4\"," +
		"\"startDate\": \"" + tomorrow + "\"," +
		"\"endDate\": \"" + tomorrow + "\"," +
		"\"reason\": \"Christmas Day 2\"" +
		"}]," +
		"\"timeZone\": \"Australia/Victoria\"" +
		"}"
	var data dirapi.Entry
	err = json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "l1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 0)
	assert.Equal(t, "l1", locSup.ID)
}

func TestGetNextSupplierClosedEventFailed(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	past := time.Now().Add(-48 * time.Hour).Format(DATE_LAYOUT)
	future := time.Now().Add(48 * time.Hour).Format(DATE_LAYOUT)
	jsonData := "{\"closures\": " +
		"[{\"id\": \"00251ffa-d517-5e1a-9a9a-a98033dda361\"," +
		"\"entry\": \"d4cd7068-a9f4-5f3b-8eea-1a169eb71eb2\"," +
		"\"startDate\": \"" + past + "\"," +
		"\"endDate\": \"" + future + "\"," +
		"\"reason\": \"Christmas Day\"" +
		"}]," +
		"\"timeZone\": \"Australia/ACT\"" +
		"}"
	var data dirapi.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.ApiDirectory), "", new(catalog.SruLookupAdapter), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), lookupAdapterFactory)
	status, result := locator.selectSupplier(appCtx, events.Event{IllTransactionID: "1"})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, [][]string{{"ISIL:SUP"}}, mockIllRepo.refreshSymbols)
	skipped, ok := result.CustomData["skippedSuppliers"].([]SkippedSupplier)
	assert.True(t, ok)
	assert.Len(t, skipped, 1)
	assert.Equal(t, "no-suppliers", result.Problem.Kind)
	assert.Equal(t, "no suppliers with new status", result.Problem.Details)
}

func TestLocateSuppliersDeduplicatesHoldingSymbolsForDirectoryLookup(t *testing.T) {
	mockIllRepo := &MockIllRepoLocateSuppliers{
		illTransaction: ill_db.IllTransaction{
			ID:          "ill-1",
			RequesterID: pgtype.Text{String: "requester-1", Valid: true},
			IllTransactionData: ill_db.IllTransactionData{
				BibliographicInfo: iso18626.BibliographicInfo{
					SupplierUniqueRecordId: "return-ISIL:SUP1::L1;return-ISIL:SUP1::L2;return-ISIL:SUP2::L3",
				},
			},
		},
		requester: ill_db.Peer{ID: "requester-1"},
		peers: []ill_db.Peer{
			{ID: "peer-1", BorrowsCount: 1},
			{ID: "peer-2", BorrowsCount: 1},
		},
		peerSymbols: map[string][]ill_db.Symbol{
			"peer-1": {{SymbolValue: "ISIL:SUP1", PeerID: "peer-1"}},
			"peer-2": {{SymbolValue: "ISIL:SUP2", PeerID: "peer-2"}},
		},
	}

	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.MockDirectoryLookupAdapter), "", new(catalog.MockLookupShared), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.MockDirectoryLookupAdapter), lookupAdapterFactory)
	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, [][]string{{"ISIL:SUP1", "ISIL:SUP2"}}, mockIllRepo.refreshSymbols)
}

func TestLocateSuppliersUsesFirstHoldingLocalIdentifierForDuplicateSymbol(t *testing.T) {
	mockIllRepo := &MockIllRepoLocateSuppliers{
		illTransaction: ill_db.IllTransaction{
			ID:          "ill-1",
			RequesterID: pgtype.Text{String: "requester-1", Valid: true},
			IllTransactionData: ill_db.IllTransactionData{
				BibliographicInfo: iso18626.BibliographicInfo{
					SupplierUniqueRecordId: "return-ISIL:SUP1::FIRST;return-ISIL:SUP1::SECOND",
				},
			},
		},
		requester: ill_db.Peer{ID: "requester-1"},
		peers: []ill_db.Peer{
			{ID: "peer-1", BorrowsCount: 1},
		},
		peerSymbols: map[string][]ill_db.Symbol{
			"peer-1": {{SymbolValue: "ISIL:SUP1", PeerID: "peer-1"}},
		},
	}

	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.MockDirectoryLookupAdapter), "", new(catalog.MockLookupShared), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.MockDirectoryLookupAdapter), lookupAdapterFactory)
	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockIllRepo.savedLocatedSuppliers, 1) {
		assert.Equal(t, "ISIL:SUP1", mockIllRepo.savedLocatedSuppliers[0].SupplierSymbol)
		assert.Equal(t, "FIRST", mockIllRepo.savedLocatedSuppliers[0].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[0].LocalID.Valid)
	}
}

func TestLocateSuppliersLastResortRequester(t *testing.T) {
	mockIllRepo := &MockIllRepoLocateSuppliers{
		illTransaction: ill_db.IllTransaction{
			ID:          "ill-1",
			RequesterID: pgtype.Text{String: "requester-1", Valid: true},
			IllTransactionData: ill_db.IllTransactionData{
				BibliographicInfo: iso18626.BibliographicInfo{
					SupplierUniqueRecordId: "return-ISIL:SUP1::L1;return-ISIL:SUP1::L2;return-ISIL:SUP2::L3",
				},
			},
		},
		requester: ill_db.Peer{ID: "requester-1", CustomData: dirapi.Entry{LenderOfLastResort: &[]dirapi.Symbol{{Authority: "ISIL", Symbol: "SUP2"}, {Symbol: "SUP3"}}}},
		peers: []ill_db.Peer{
			{ID: "peer-1", BorrowsCount: 1},
			{ID: "peer-2", BorrowsCount: 1},
			{ID: "peer-3", BorrowsCount: 1},
		},
		peerSymbols: map[string][]ill_db.Symbol{
			"peer-1": {{SymbolValue: "ISIL:SUP1", PeerID: "peer-1"}},
			"peer-2": {{SymbolValue: "ISIL:SUP2", PeerID: "peer-2"}},
			"peer-3": {{SymbolValue: "ISIL:SUP3", PeerID: "peer-3"}},
		},
	}

	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.MockDirectoryLookupAdapter), "", new(catalog.MockLookupShared), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.MockDirectoryLookupAdapter), lookupAdapterFactory)
	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, [][]string{{"ISIL:SUP1", "ISIL:SUP2", "ISIL:SUP3"}}, mockIllRepo.refreshSymbols)

	if assert.Len(t, mockIllRepo.savedLocatedSuppliers, 3) {
		assert.Equal(t, "ISIL:SUP1", mockIllRepo.savedLocatedSuppliers[0].SupplierSymbol)
		assert.Equal(t, "L1", mockIllRepo.savedLocatedSuppliers[0].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[0].LocalID.Valid)

		assert.Equal(t, "ISIL:SUP2", mockIllRepo.savedLocatedSuppliers[1].SupplierSymbol)
		assert.Equal(t, "L3", mockIllRepo.savedLocatedSuppliers[1].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[1].LocalID.Valid)

		assert.Equal(t, "ISIL:SUP3", mockIllRepo.savedLocatedSuppliers[2].SupplierSymbol)
		assert.Equal(t, "return-ISIL:SUP1::L1;return-ISIL:SUP1::L2;return-ISIL:SUP2::L3", mockIllRepo.savedLocatedSuppliers[2].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[2].LocalID.Valid)
	}
}

func TestLocateSuppliersLastResortLookupEmpty(t *testing.T) {
	mockIllRepo := &MockIllRepoLocateSuppliers{
		illTransaction: ill_db.IllTransaction{
			ID:          "ill-1",
			RequesterID: pgtype.Text{String: "requester-1", Valid: true},
			IllTransactionData: ill_db.IllTransactionData{
				BibliographicInfo: iso18626.BibliographicInfo{
					SupplierUniqueRecordId: "not-found",
				},
			},
		},
		requester: ill_db.Peer{ID: "requester-1", CustomData: dirapi.Entry{LenderOfLastResort: &[]dirapi.Symbol{{Authority: "ISIL", Symbol: "SUP2"}, {Symbol: "SUP3"}}}},
		peers: []ill_db.Peer{
			{ID: "peer-2", BorrowsCount: 1},
			{ID: "peer-3", BorrowsCount: 1},
		},
		peerSymbols: map[string][]ill_db.Symbol{
			"peer-2": {{SymbolValue: "ISIL:SUP2", PeerID: "peer-2"}},
			"peer-3": {{SymbolValue: "ISIL:SUP3", PeerID: "peer-3"}},
		},
	}

	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.MockDirectoryLookupAdapter), "", new(catalog.MockLookupShared), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.MockDirectoryLookupAdapter), lookupAdapterFactory)
	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, [][]string{{"ISIL:SUP2", "ISIL:SUP3"}}, mockIllRepo.refreshSymbols)

	if assert.Len(t, mockIllRepo.savedLocatedSuppliers, 2) {
		assert.Equal(t, "ISIL:SUP2", mockIllRepo.savedLocatedSuppliers[0].SupplierSymbol)
		assert.Equal(t, "not-found", mockIllRepo.savedLocatedSuppliers[0].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[0].LocalID.Valid)

		assert.Equal(t, "ISIL:SUP3", mockIllRepo.savedLocatedSuppliers[1].SupplierSymbol)
		assert.Equal(t, "not-found", mockIllRepo.savedLocatedSuppliers[1].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[1].LocalID.Valid)
	}
}

func TestLocateSuppliersLastResortConsortium(t *testing.T) {
	mockIllRepo := &MockIllRepoLocateSuppliers{
		illTransaction: ill_db.IllTransaction{
			ID:          "ill-1",
			RequesterID: pgtype.Text{String: "requester-1", Valid: true},
			IllTransactionData: ill_db.IllTransactionData{
				BibliographicInfo: iso18626.BibliographicInfo{
					SupplierUniqueRecordId: "return-ISIL:SUP1::L1",
				},
			},
		},
		requester: ill_db.Peer{ID: "requester-1"},
		peers: []ill_db.Peer{
			{ID: "peer-1", BorrowsCount: 1},
			{ID: "peer-2", BorrowsCount: 1},
		},
		peerSymbols: map[string][]ill_db.Symbol{
			"peer-1": {{SymbolValue: "ISIL:SUP1", PeerID: "peer-1"}},
			"peer-2": {{SymbolValue: "ISIL:SUP2", PeerID: "peer-2"}},
		},
		consortiumPeers: []ill_db.Peer{
			{ID: "consortium-peer-1", CustomData: dirapi.Entry{Symbols: &[]dirapi.Symbol{{Authority: "ISIL", Symbol: "SUPC"}}, LenderOfLastResort: &[]dirapi.Symbol{{Authority: "ISIL", Symbol: "SUP2"}}}},
		},
	}

	lookupAdapterFactory := NewLookupAdapterFactory(mockIllRepo, new(adapter.MockDirectoryLookupAdapter), "ISIL:SUPC", new(catalog.MockLookupShared), new(catalog.LookupAdapterCreatorImpl))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.MockDirectoryLookupAdapter), lookupAdapterFactory)
	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, [][]string{{"ISIL:SUP1", "ISIL:SUP2"}}, mockIllRepo.refreshSymbols)
	if assert.Len(t, mockIllRepo.savedLocatedSuppliers, 2) {
		assert.Equal(t, "ISIL:SUP1", mockIllRepo.savedLocatedSuppliers[0].SupplierSymbol)
		assert.Equal(t, "L1", mockIllRepo.savedLocatedSuppliers[0].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[0].LocalID.Valid)

		assert.Equal(t, "ISIL:SUP2", mockIllRepo.savedLocatedSuppliers[1].SupplierSymbol)
		assert.Equal(t, "return-ISIL:SUP1::L1", mockIllRepo.savedLocatedSuppliers[1].LocalID.String)
		assert.True(t, mockIllRepo.savedLocatedSuppliers[1].LocalID.Valid)
	}
}

type MockIllRepoLocateSuppliers struct {
	mocks.MockIllRepositorySuccess
	illTransaction        ill_db.IllTransaction
	requester             ill_db.Peer
	peers                 []ill_db.Peer
	peerSymbols           map[string][]ill_db.Symbol
	refreshSymbols        [][]string
	savedLocatedSuppliers []ill_db.SaveLocatedSupplierParams
	consortiumPeers       []ill_db.Peer
}

func (r *MockIllRepoLocateSuppliers) GetIllTransactionById(ctx common.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return r.illTransaction, nil
}

func (r *MockIllRepoLocateSuppliers) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	return r.requester, nil
}

func (r *MockIllRepoLocateSuppliers) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	if len(r.consortiumPeers) > 0 && r.consortiumPeers[0].CustomData.Symbols != nil {
		for _, sym := range *r.consortiumPeers[0].CustomData.Symbols {
			for _, s := range symbols {
				if sym.Authority+":"+sym.Symbol == s {
					return r.consortiumPeers, "<refresh>", nil
				}
			}
		}
	}
	r.refreshSymbols = append(r.refreshSymbols, append([]string(nil), symbols...))
	return r.peers, "<refresh>", nil
}

func (r *MockIllRepoLocateSuppliers) GetSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.Symbol, error) {
	return r.peerSymbols[peerId], nil
}

func (r *MockIllRepoLocateSuppliers) GetExclusiveBranchSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.BranchSymbol, error) {
	return []ill_db.BranchSymbol{}, nil
}

func (r *MockIllRepoLocateSuppliers) SaveLocatedSupplier(ctx common.ExtendedContext, params ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	if params.SupplierStatus != ill_db.SupplierStateSkippedPg {
		r.savedLocatedSuppliers = append(r.savedLocatedSuppliers, params)
	}
	return ill_db.LocatedSupplier(params), nil
}

type MockIllRepoRequester struct {
	mocks.MockIllRepositorySuccess
	refreshSymbols [][]string
	refreshErr     error
}

func (r *MockIllRepoRequester) GetPeerById(ctx common.ExtendedContext, peerId string) (ill_db.Peer, error) {
	args := r.Called(peerId)
	return args.Get(0).(ill_db.Peer), args.Error(1)
}

func (r *MockIllRepoRequester) GetLocatedSuppliersByIllTransactionAndStatus(ctx common.ExtendedContext, params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	if params.SupplierStatus == ill_db.SupplierStateNewPg {
		return []ill_db.LocatedSupplier{{ID: "1", SupplierID: "p1", SupplierSymbol: "ISIL:SUP"}}, nil
	}
	return []ill_db.LocatedSupplier{}, nil
}

func (r *MockIllRepoRequester) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	r.refreshSymbols = append(r.refreshSymbols, symbols)
	return []ill_db.Peer{}, "<refresh>", r.refreshErr
}

// MockIllRepoLocateSuppliersWithSave extends MockIllRepoLocateSuppliers with tracking of
// SaveIllTransaction calls, allowing the metadata-update tests to verify what was persisted.
type MockIllRepoLocateSuppliersWithSave struct {
	MockIllRepoLocateSuppliers
	savedTransactions  []ill_db.SaveIllTransactionParams
	saveTransactionErr error
}

func (r *MockIllRepoLocateSuppliersWithSave) SaveIllTransaction(ctx common.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	if r.saveTransactionErr != nil {
		return ill_db.IllTransaction{}, r.saveTransactionErr
	}
	r.savedTransactions = append(r.savedTransactions, params)
	return ill_db.IllTransaction{IllTransactionData: params.IllTransactionData}, nil
}

// metadataTestRepo builds a MockIllRepoLocateSuppliersWithSave pre-wired with a single
// ISIL:SUP1 supplier peer so the holdings lookup completes successfully.
func metadataTestRepo(illTrans ill_db.IllTransaction, requester ill_db.Peer) *MockIllRepoLocateSuppliersWithSave {
	return &MockIllRepoLocateSuppliersWithSave{
		MockIllRepoLocateSuppliers: MockIllRepoLocateSuppliers{
			illTransaction: illTrans,
			requester:      requester,
			peers:          []ill_db.Peer{{ID: "peer-1", BorrowsCount: 1}},
			peerSymbols: map[string][]ill_db.Symbol{
				"peer-1": {{SymbolValue: "ISIL:SUP1", PeerID: "peer-1"}},
			},
		},
	}
}

// metadataTestRequester returns a peer carrying the given MetadataUpdateMode in its HoldingsConfig.
// Pass nil to leave HoldingsConfig absent (mode defaults to None).
func metadataTestRequester(mode *dirapi.MetadataUpdateMode) ill_db.Peer {
	var cc *dirapi.HoldingsConfig
	if mode != nil {
		cc = &dirapi.HoldingsConfig{MetadataUpdateMode: mode}
	}
	return ill_db.Peer{
		ID:         "requester-1",
		CustomData: dirapi.Entry{Name: "test-requester", HoldingsConfig: cc},
	}
}

func TestLocateSuppliersMetadataModeNoneSkipsUpdate(t *testing.T) {
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "some-id",
				Title:                  "Original Title",
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(nil)) // no HoldingsConfig → mode=None
	holdingsAdapter := &catalog.MockLookupAdapter{
		Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Empty(t, mockRepo.savedTransactions, "SaveIllTransaction should not be called when mode is None")
}

func TestLocateSuppliersMetadataSkippedForCrossLinkVendor(t *testing.T) {
	mode := dirapi.Merge
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "some-id",
				Title:                  "Original Title",
			},
		},
	}
	requester := metadataTestRequester(&mode)
	requester.Vendor = string(dirapi.CrossLink) // CrossLink vendor bypasses the metadata update
	mockRepo := metadataTestRepo(illTrans, requester)
	holdingsAdapter := &catalog.MockLookupAdapter{
		Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Empty(t, mockRepo.savedTransactions, "SaveIllTransaction should not be called for CrossLink vendor")
}

func TestLocateSuppliersMetadataMergePopulatesEmptyFields(t *testing.T) {
	mode := dirapi.Merge
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "some-id",
				// Title intentionally empty so Merge fills it in
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(&mode))
	holdingsAdapter := &catalog.MockLookupAdapter{
		Metadata: catalog.Metadata{Title: "Catalog Title", Author: "Catalog Author"},
		Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockRepo.savedTransactions, 1) {
		saved := mockRepo.savedTransactions[0].IllTransactionData.BibliographicInfo
		assert.Equal(t, "Catalog Title", saved.Title)
		assert.Equal(t, "Catalog Author", saved.Author)
	}
}

func TestLocateSuppliersMetadataMergePreservesExistingFields(t *testing.T) {
	mode := dirapi.Merge
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "some-id",
				Title:                  "Patron Title", // already set → Merge should not overwrite
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(&mode))
	holdingsAdapter := &catalog.MockLookupAdapter{
		Metadata: catalog.Metadata{Title: "Catalog Title"},
		Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockRepo.savedTransactions, 1) {
		saved := mockRepo.savedTransactions[0].IllTransactionData.BibliographicInfo
		assert.Equal(t, "Patron Title", saved.Title) // preserved by Merge
	}
}

func TestLocateSuppliersMetadataAutoWithIdentifierReplaces(t *testing.T) {
	mode := dirapi.Auto
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "record-123", // non-empty → Auto resolves to Replace
				Title:                  "Old Title",
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(&mode))
	holdingsAdapter := &catalog.MockLookupAdapter{
		Metadata: catalog.Metadata{Title: "Catalog Title"},
		Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockRepo.savedTransactions, 1) {
		saved := mockRepo.savedTransactions[0].IllTransactionData.BibliographicInfo
		assert.Equal(t, "Catalog Title", saved.Title) // replaced
	}
}

func TestLocateSuppliersMetadataAutoWithoutIdentifierMerges(t *testing.T) {
	mode := dirapi.Auto
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				// SupplierUniqueRecordId intentionally empty → Auto resolves to Merge
				Title: "Patron Title",
				BibliographicItemId: []iso18626.BibliographicItemId{
					{
						BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
						BibliographicItemIdentifier:     "978-0-123",
					},
				},
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(&mode))
	holdingsAdapter := &catalog.MockLookupAdapter{
		Metadata: catalog.Metadata{Title: "Catalog Title"},
		Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockRepo.savedTransactions, 1) {
		saved := mockRepo.savedTransactions[0].IllTransactionData.BibliographicInfo
		assert.Equal(t, "Patron Title", saved.Title) // preserved by Merge
	}
}

func TestLocateSuppliersMetadataLookupError(t *testing.T) {
	mode := dirapi.Merge
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "some-id",
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(&mode))
	holdingsAdapter := &catalog.MockLookupAdapter{
		Err: errors.New("metadata lookup failed"),
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Empty(t, mockRepo.savedTransactions)
}

func TestLocateSuppliersMetadataSaveTransactionError(t *testing.T) {
	mode := dirapi.Merge
	illTrans := ill_db.IllTransaction{
		ID:          "ill-1",
		RequesterID: pgtype.Text{String: "requester-1", Valid: true},
		IllTransactionData: ill_db.IllTransactionData{
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "some-id",
			},
		},
	}
	mockRepo := metadataTestRepo(illTrans, metadataTestRequester(&mode))
	mockRepo.saveTransactionErr = errors.New("db error")
	holdingsAdapter := &catalog.MockLookupAdapter{
		Metadata: catalog.Metadata{Title: "Catalog Title"},
	}
	factory := NewLookupAdapterFactory(mockRepo, new(adapter.MockDirectoryLookupAdapter), "", holdingsAdapter, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockRepo, new(adapter.MockDirectoryLookupAdapter), factory)

	status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})

	assert.Equal(t, events.EventStatusError, status)
}
