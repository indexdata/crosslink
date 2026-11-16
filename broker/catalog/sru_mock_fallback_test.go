package catalog

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/indexdata/crosslink/illmock/sruapi"
	"github.com/indexdata/crosslink/marcxml"
	"github.com/indexdata/crosslink/sru"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSruMockBeforeCatalog(t *testing.T) {
	const clusterID = "429e871b-9cd3-465a-8d94-a82bac2d9a44"
	marcRecord, err := xml.Marshal(marcxml.Record{Datafield: []marcxml.DataFieldType{
		{Tag: "245", Subfield: []marcxml.SubfieldatafieldType{{Code: "a", Text: "Reservoir title"}}},
		{Tag: "999", Ind1: "1", Ind2: "1", Subfield: []marcxml.SubfieldatafieldType{
			{Code: "l", Text: "reservoir-local"},
			{Code: "s", Text: "ISIL:US-RS2"},
		}},
	}})
	require.NoError(t, err)
	catalogResponse, err := xml.Marshal(sru.SearchRetrieveResponse{
		SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
			NumberOfRecords: 1,
			Records: &sru.RecordsDefinition{Record: []sru.RecordDefinition{{
				RecordSchema: "marcxml",
				RecordData:   sru.StringOrXmlFragmentDefinition{XMLContent: marcRecord},
			}}},
		},
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name         string
		identifier   string
		wantSymbol   string
		wantLocalID  string
		wantFallback int32
	}{
		{"test identifier", "return-ISIL:US-RS1::whatever", "ISIL:US-RS1", "whatever", 0},
		{"cluster UUID", clusterID, "ISIL:US-RS2", "reservoir-local", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := httptest.NewServer(sruapi.CreateSruApi().HttpHandler())
			defer mock.Close()
			var fallbackCalls atomic.Int32
			catalog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls.Add(1)
				query, err := encodeCqlSearchClause("rec.id", tc.identifier)
				assert.NoError(t, err)
				assert.Equal(t, query, r.URL.Query().Get("query"))
				w.Header().Set("Content-Type", "application/xml")
				_, err = w.Write(catalogResponse)
				assert.NoError(t, err)
			}))
			defer catalog.Close()

			adapter := createSruAdapter(t, false, mock.URL, catalog.URL)
			result, err := adapter.Lookup(LookupParams{Identifier: tc.identifier})
			require.NoError(t, err)
			holdings, err := result.GetHoldings()
			require.NoError(t, err)
			require.Len(t, holdings, 1)
			assert.Equal(t, tc.wantSymbol, holdings[0].Symbol)
			assert.Equal(t, tc.wantLocalID, holdings[0].LocalIdentifier)
			assert.Equal(t, tc.wantFallback, fallbackCalls.Load())
			if tc.wantFallback > 0 {
				metadata, err := result.GetMetadata()
				require.NoError(t, err)
				assert.Equal(t, "Reservoir title", metadata.Title)
			}
		})
	}
}
