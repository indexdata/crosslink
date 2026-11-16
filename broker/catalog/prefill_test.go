package catalog

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/sru"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefillRecordPresenceAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name    string
		records [][]byte
		found   bool
		title   string
	}{
		{"no record", nil, false, ""},
		{"empty record", [][]byte{[]byte(`<record/>`)}, true, ""},
		{"first metadata without holdings", [][]byte{[]byte(`<record><datafield tag="245"><subfield code="a">First</subfield></datafield></record>`), []byte(`<record><datafield tag="245"><subfield code="a">Second</subfield></datafield></record>`)}, true, "First"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries = append(queries, r.URL.Query().Get("query"))
				records := &sru.RecordsDefinition{}
				for _, record := range tc.records {
					records.Record = append(records.Record, sru.RecordDefinition{RecordSchema: "marcxml", RecordData: sru.StringOrXmlFragmentDefinition{XMLContent: record}})
				}
				response := sru.SearchRetrieveResponse{SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{NumberOfRecords: uint64(len(tc.records)), Records: records}}
				w.Header().Set("Content-Type", "application/xml")
				require.NoError(t, xml.NewEncoder(w).Encode(response))
			}))
			defer server.Close()
			queryType := dirapi.QueryConfigTypeCql
			builder, err := NewQueryBuilderGen(&dirapi.QueryConfig{Type: &queryType})
			require.NoError(t, err)
			adapter := CreateSruLookupAdapter(server.Client(), []string{server.URL}, "", builder, NewMarcHoldingsParser(dirapi.MarcHoldingsParserConfig{}), NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{}), "marcxml")
			result, err := LookupContext(context.Background(), adapter, LookupParams{Identifier: "record", Isbn: "9783161484100", Issn: "1234567X", ServiceType: "Copy"})
			require.NoError(t, err)
			assert.Equal(t, tc.found, result.HasRecord())
			metadata, err := result.GetMetadata()
			require.NoError(t, err)
			assert.Equal(t, tc.title, metadata.Title)
			holdings, err := result.GetHoldings()
			require.NoError(t, err)
			assert.Empty(t, holdings)
			require.Len(t, queries, 3)
			assert.Contains(t, queries[0], "rec.id")
			assert.Contains(t, queries[1], "isbn")
			assert.Contains(t, queries[2], "issn")
		})
	}
}

func TestPrefillContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	adapter := createSruAdapter(t, false, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := LookupContext(ctx, adapter, LookupParams{Identifier: "record"}); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("lookup did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("lookup ignored cancellation")
	}
}

func TestPrefillConcurrentContexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		response := sru.SearchRetrieveResponse{SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{NumberOfRecords: 0}}
		_ = xml.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	adapter := createSruAdapter(t, false, server.URL)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := LookupContext(ctx, adapter, LookupParams{Identifier: "record"})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

func TestPrefillClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client := server.Client()
	client.Timeout = 20 * time.Millisecond
	adapter := CreateSruLookupAdapter(client, []string{server.URL}, "", NewQueryBuilderIsxn(false), NewMarcHoldingsParser(dirapi.MarcHoldingsParserConfig{}), nil, "marcxml")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := LookupContext(ctx, adapter, LookupParams{Identifier: "record"})
	require.Error(t, err)
	require.NoError(t, ctx.Err(), "the shorter client timeout must be preserved")
}

func TestPrefillCancelledAndMetaproxy(t *testing.T) {
	adapter := &MetaproxyLookupAdapter{holdingsLookupAdapter: &MockLookupAdapter{RecordFound: true}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LookupContext(ctx, adapter, LookupParams{Identifier: "record"})
	require.ErrorIs(t, err, context.Canceled)
	result, err := LookupContext(context.Background(), adapter, LookupParams{Identifier: "record"})
	require.NoError(t, err)
	require.True(t, result.HasRecord())
}
