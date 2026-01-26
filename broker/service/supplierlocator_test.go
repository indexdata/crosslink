package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/directory"
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
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 1)
	assert.Equal(t, "", locSup.ID)
}

func TestGetNextSupplierFailToLoadPeer(t *testing.T) {
	peerId := "p1"
	mockIllRepo := new(MockIllRepoRequester)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{}, errors.New("db error"))
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

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
	var data directory.Entry
	err := json.Unmarshal([]byte(jsonData), &data)
	assert.NoError(t, err)
	mockIllRepo.On("GetPeerById", peerId).Return(ill_db.Peer{CustomData: data}, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), mockIllRepo, new(adapter.ApiDirectory), new(adapter.SruHoldingsLookupAdapter))

	locSup, skipped, err := locator.getNextSupplier(appCtx, []ill_db.LocatedSupplier{{ID: "1", SupplierID: peerId}})
	assert.NoError(t, err)
	assert.Len(t, skipped, 1)
	assert.Equal(t, "", locSup.ID)
}

type MockIllRepoRequester struct {
	mocks.MockIllRepositorySuccess
}

func (r *MockIllRepoRequester) GetPeerById(ctx common.ExtendedContext, peerId string) (ill_db.Peer, error) {
	args := r.Called(peerId)
	return args.Get(0).(ill_db.Peer), args.Error(1)
}
