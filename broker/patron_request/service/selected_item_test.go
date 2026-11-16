package prservice

import (
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/require"
)

func TestSkippedRequestItemAttachesSelectedCopy(t *testing.T) {
	for _, adapterType := range []string{"manual", "ncip", "ncip1"} {
		t.Run(adapterType, func(t *testing.T) {
			var adapter lms.LmsAdapter = &lms.LmsAdapterManual{}
			config := dirapi.LmsConfig{Address: "http://unused.invalid", FromAgency: "MAIN", RequestItemEnabled: new(false)}
			var err error
			switch adapterType {
			case "ncip":
				adapter, err = lms.CreateLmsAdapterNcip(config)
			case "ncip1":
				adapter, err = lms.CreateLmsAdapterNcip1(config)
			}
			require.NoError(t, err)
			repo := new(MockPrRepo)
			service := &PatronRequestActionService{prRepo: repo}
			request := iso18626.Request{
				Header:            iso18626.Header{RequestingAgencyRequestId: "req-1"},
				BibliographicInfo: iso18626.BibliographicInfo{Title: "Selected title", SupplierUniqueRecordId: "bib"},
				SupplierInfo:      []iso18626.SupplierInfo{{BibliographicRecordId: &iso18626.BibliographicRecordId{BibliographicRecordIdentifier: "copy"}, CallNumber: "QA 76"}},
			}
			pr := pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")}
			result := service.requestItemLenderRequest(appCtx, pr, adapter, request)
			require.Equal(t, events.EventStatusSuccess, result.status)
			require.Len(t, repo.savedItems, 1)
			item := repo.savedItems[0]
			require.Equal(t, "copy", item.Barcode)
			require.Equal(t, "copy", item.ItemID.String)
			require.Equal(t, "QA 76", item.CallNumber.String)
			require.Equal(t, "Selected title", item.Title.String)
			require.False(t, item.LmsRequestID.Valid)
			require.False(t, item.LmsItemID.Valid)
			require.Equal(t, pr_db.LmsStatusUnknown, item.LmsStatus)
			result = service.requestItemLenderRequest(appCtx, pr, adapter, request)
			require.Equal(t, events.EventStatusSuccess, result.status)
			require.Len(t, repo.savedItems, 1)
			// Staff may replace the selected copy. A retry must retain that decision.
			repo.savedItems[0].Barcode = "staff-copy"
			result = service.requestItemLenderRequest(appCtx, pr, adapter, request)
			require.Equal(t, events.EventStatusSuccess, result.status)
			require.Len(t, repo.savedItems, 1)
			require.Equal(t, "staff-copy", repo.savedItems[0].Barcode)
		})
	}
}

func TestSkippedRequestItemSaveFailure(t *testing.T) {
	repo := &MockPrRepo{saveItemFail: true}
	service := &PatronRequestActionService{prRepo: repo}
	request := iso18626.Request{SupplierInfo: []iso18626.SupplierInfo{{BibliographicRecordId: &iso18626.BibliographicRecordId{BibliographicRecordIdentifier: "copy"}}}}
	result := service.requestItemLenderRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId}, &lms.LmsAdapterManual{}, request)
	require.Equal(t, events.EventStatusError, result.status)
	require.Empty(t, repo.savedItems)
}
