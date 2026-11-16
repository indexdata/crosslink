package prservice

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/crosslink/ncip"
	ncip1 "github.com/indexdata/crosslink/ncipv1"
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

func TestIncomingSelectedItemHonorsBibliographicScope(t *testing.T) {
	for _, version := range []string{"1.0", "2.02"} {
		for _, scope := range []string{"Title", "Bibliographic Item"} {
			for _, bib := range []string{"", "bib"} {
				t.Run(version+"/"+scope+"/"+bib, func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						assertBibliographicNcipRequest(t, r, version, scope, bib)
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
					if bib == "" {
						require.Contains(t, result.result.EventError.Cause, "missing identifier")
						require.Zero(t, calls.Load())
					} else {
						require.Equal(t, "LMS RequestItem failed", result.result.EventError.Message)
						require.Equal(t, int32(1), calls.Load())
					}
					repo.AssertExpectations(t)
				})
			}
		}
	}
}

// assertBibliographicNcipRequest checks identifiers before the server injects an error.
func assertBibliographicNcipRequest(t *testing.T, r *http.Request, version, scope, bib string) {
	t.Helper()
	if version == "1.0" {
		var message ncip1.NCIPMessage
		require.NoError(t, xml.NewDecoder(r.Body).Decode(&message))
		req := message.RequestItem
		require.NotNil(t, req)
		require.Equal(t, scope, req.RequestScopeType.Value)
		require.Nil(t, req.UniqueItemId)
		require.NotNil(t, req.UniqueBibliographicId)
		require.Equal(t, bib, req.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifier)
	} else {
		var message ncip.NCIPMessage
		require.NoError(t, xml.NewDecoder(r.Body).Decode(&message))
		req := message.RequestItem
		require.NotNil(t, req)
		require.Equal(t, scope, req.RequestScopeType.Text)
		require.Empty(t, req.ItemId)
		require.Len(t, req.BibliographicId, 1)
		require.Equal(t, bib, req.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
	}
}
