package prservice

import (
	"encoding/xml"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedIndexRecordIDExchange(t *testing.T) {
	for _, id := range []string{"9a18bd66-8db6-4afd-95e6-157b2d3b6d12", "36457383", ""} {
		t.Run(id, func(t *testing.T) {
			request := iso18626.Request{
				BibliographicInfo: iso18626.BibliographicInfo{
					SupplierUniqueRecordId: id,
					BibliographicRecordId: []iso18626.BibliographicRecordId{
						{BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: "OCLC"}, BibliographicRecordIdentifier: "oclc-id"},
						{BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ReShare"}, BibliographicRecordIdentifier: "stale-index-id"},
					},
				},
				SupplierInfo: []iso18626.SupplierInfo{{BibliographicRecordId: &iso18626.BibliographicRecordId{BibliographicRecordIdentifier: "local-copy"}}},
			}
			sender := PatronRequestMessageSender{eventBus: new(MockEventBus), iso18626Handler: new(MockIso18626Handler)}
			status, result, err := sender.sendBorrowingRequest(appCtx, "send", pr_db.PatronRequest{
				ID: "request", RequesterSymbol: getDbText("ISIL:REQ1"),
			}, request)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusSuccess, status)
			// Exercise the wire format used between brokers and suppliers.
			wire, err := xml.Marshal(result.OutgoingMessage)
			require.NoError(t, err)
			var received iso18626.ISO18626Message
			require.NoError(t, xml.Unmarshal(wire, &received))
			require.NotNil(t, received.Request)
			records := received.Request.BibliographicInfo.BibliographicRecordId
			require.NotEmpty(t, records)
			assert.Equal(t, "OCLC", records[0].BibliographicRecordIdentifierCode.Text)
			assert.Equal(t, "oclc-id", records[0].BibliographicRecordIdentifier)
			if id == "" {
				require.Len(t, records, 1)
			} else {
				require.Len(t, records, 2)
				assert.Equal(t, "ReShare", records[1].BibliographicRecordIdentifierCode.Text)
				assert.Equal(t, id, records[1].BibliographicRecordIdentifier)
			}
			repo := new(MockPrRepo)
			created, err := CreateLendingRequest(appCtx, repo, pr_db.PatronRequest{ID: "supply", Side: SideLending, IllRequest: *received.Request})
			require.NoError(t, err)
			assert.Equal(t, id, BibRecordID(SideLending, created.IllRequest))
			assert.Equal(t, "local-copy", created.IllRequest.SupplierInfo[0].BibliographicRecordId.BibliographicRecordIdentifier)
			assert.Equal(t, "stale-index-id", request.BibliographicInfo.BibliographicRecordId[1].BibliographicRecordIdentifier)
		})
	}
}

func TestLendingRequestReShareRecordIDs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		codes, ids []string
		expected   string
		invalid    bool
	}{
		{name: "no shared index ID", codes: []string{"OCLC", "Local"}, ids: []string{"1", "2"}},
		{name: "duplicate same ID", codes: []string{"ReShare", "reshare"}, ids: []string{"1", "1"}, expected: "1"},
		{name: "empty ignored", codes: []string{"ReShare", "ReShare"}, ids: []string{"", "1"}, expected: "1"},
		{name: "conflicting IDs rejected", codes: []string{"ReShare", "ReShare"}, ids: []string{"1", "2"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := iso18626.Request{}
			for i, code := range tc.codes {
				request.BibliographicInfo.BibliographicRecordId = append(request.BibliographicInfo.BibliographicRecordId, iso18626.BibliographicRecordId{
					BibliographicRecordIdentifierCode: iso18626.TypeSchemeValuePair{Text: code}, BibliographicRecordIdentifier: tc.ids[i],
				})
			}
			repo := new(MockPrRepo)
			created, err := CreateLendingRequest(appCtx, repo, pr_db.PatronRequest{ID: "supply", Side: SideLending, IllRequest: request})
			if tc.invalid {
				require.EqualError(t, err, "conflicting ReShare bibliographic record IDs")
				assert.Empty(t, repo.createdPr.ID)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, BibRecordID(SideLending, created.IllRequest))
			}
		})
	}
}
