package prservice

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/require"
)

func TestRequestItemIdentifierFromISO(t *testing.T) {
	for _, tc := range []struct {
		name    string
		items   []string
		want    string
		invalid bool
	}{
		{name: "bibliographic only"},
		{name: "selected copy", items: []string{"copy"}, want: "copy"},
		{name: "duplicate identical", items: []string{"copy", "copy"}, want: "copy"},
		{name: "conflicting copies", items: []string{"copy", "other"}, invalid: true},
		{name: "empty copy", items: []string{""}, invalid: true},
		{name: "blank copy", items: []string{"  "}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "bib"}}
			for _, item := range tc.items {
				request.SupplierInfo = append(request.SupplierInfo, iso18626.SupplierInfo{BibliographicRecordId: &iso18626.BibliographicRecordId{BibliographicRecordIdentifier: item}})
			}
			id, err := requestItemIdentifierFromISO(request)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, lms.RequestItemIdentifier{BibliographicID: "bib", ItemID: tc.want}, id)
			}
		})
	}
}

func TestIncomingSelectedItemRejectsBibliographicScope(t *testing.T) {
	for _, version := range []string{"1.0", "2.02"} {
		for _, scope := range []string{"Title", "Bibliographic Item"} {
			for _, bib := range []string{"", "bib"} {
				t.Run(version+"/"+scope+"/"+bib, func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						calls.Add(1)
						w.WriteHeader(http.StatusInternalServerError)
					}))
					defer server.Close()
					adapter, err := lms.CreateLmsAdapterNcip(dirapi.LmsConfig{Address: server.URL, FromAgency: "broker", NcipVersion: &version, RequestItemRequestScopeType: &scope})
					require.NoError(t, err)
					pr := pr_db.PatronRequest{ID: "pr", RequesterSymbol: getDbText("ISIL:REQUESTER")}
					repo := new(MockPrRepo)
					repo.On("GetItemsByPrId", pr.ID).Return([]pr_db.Item{}, nil).Once()
					svc := &PatronRequestActionService{prRepo: repo}
					request := iso18626.Request{
						Header:            iso18626.Header{RequestingAgencyRequestId: "request"},
						BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: bib},
						SupplierInfo:      []iso18626.SupplierInfo{{BibliographicRecordId: &iso18626.BibliographicRecordId{BibliographicRecordIdentifier: "selected-copy"}}},
					}
					result := svc.requestItemLenderRequest(appCtx, pr, adapter, request)
					require.Equal(t, events.EventStatusError, result.status)
					require.NotNil(t, result.result.EventError)
					require.Contains(t, result.result.EventError.Cause, "selected item requires NCIP Item scope")
					require.Zero(t, calls.Load(), "a selected copy must not be broadened into a title reservation")
					repo.AssertExpectations(t)
				})
			}
		}
	}
}
