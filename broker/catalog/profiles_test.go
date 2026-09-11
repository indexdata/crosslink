package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
)

func profileParser(t *testing.T, name string) HoldingsParser {
	t.Helper()
	var entry dirapi.Entry
	require.NoError(t, json.Unmarshal([]byte(`{"catalogConfig":{"profile":"`+name+`"}}`), &entry))
	e, err := profiles.Resolve(entry)
	require.NoError(t, err)
	p, err := getHoldingsParser(e.Catalog.HoldingsFormat)
	require.NoError(t, err)
	return p
}
func TestKohaAvailability(t *testing.T) {
	p := profileParser(t, "Koha")
	for _, tc := range []struct {
		sub       string
		available bool
	}{
		{`<subfield code="7">0</subfield>`, true},
		{``, false}, {`<subfield code="7">1</subfield>`, false},
		{`<subfield code="7">0</subfield><subfield code="q"/>`, false},
		{`<subfield code="7">0</subfield><subfield code="q">2026-01-01</subfield>`, false},
	} {
		record := `<record xmlns="http://www.loc.gov/MARC21/slim"><datafield tag="952"><subfield code="b">MAIN</subfield><subfield code="c">STACKS</subfield><subfield code="o">QA1</subfield><subfield code="p">IGNORED</subfield>` + tc.sub + `</datafield></record>`
		h, err := p.Parse([]byte(record), LookupParams{})
		require.NoError(t, err)
		if tc.available {
			require.Len(t, h, 1)
			require.Equal(t, Holding{Location: "MAIN", ShelvingLocation: "STACKS", CallNumber: "QA1"}, h[0])
		} else {
			require.Empty(t, h)
		}
	}
}
func TestSierraAvailability(t *testing.T) {
	p := profileParser(t, "Sierra")
	for _, note := range []string{"AVAILABLE", "CHECK SHELVES", "CHECK SHELF", "available", " AVAILABLE", "ON LOAN", ""} {
		h, err := p.Parse([]byte(`<opacRecord><holdings><holding><localLocation>MAIN</localLocation><shelvingLocation>IGNORED</shelvingLocation><callNumber>QA1</callNumber><publicNote>`+note+`</publicNote></holding></holdings></opacRecord>`), LookupParams{})
		require.NoError(t, err)
		if note == "AVAILABLE" || note == "CHECK SHELVES" || note == "CHECK SHELF" {
			require.Equal(t, []Holding{{Location: "MAIN", ShelvingLocation: "MAIN", CallNumber: "QA1"}}, h)
		} else {
			require.Empty(t, h)
		}
	}
}
func TestOpacProfiles(t *testing.T) {
	record := `<opacRecord><holdings><holding><localLocation>MAIN</localLocation><shelvingLocation>STACKS</shelvingLocation><callNumber>QA1</callNumber><circulations><circulation><availableNow value="0"/></circulation><circulation><availableNow value="1"/><itemId>1</itemId><availableThru> LOAN </availableThru><temporaryLocation>TEMP</temporaryLocation></circulation><circulation><availableNow value="1"/><itemId>2</itemId></circulation></circulations></holding><holding><localLocation/><circulations><circulation><availableNow value="1"/></circulation></circulations></holding><holding/></holdings></opacRecord>`
	for _, name := range []string{"Alma", "FOLIO"} {
		h, err := profileParser(t, name).Parse([]byte(record), LookupParams{})
		require.NoError(t, err)
		require.Len(t, h, 2)
		require.Equal(t, "1", h[0].ItemId)
		require.Equal(t, "LOAN", h[0].ItemLoanPolicy)
		require.Equal(t, "STACKS", h[0].ShelvingLocation)
		if name == "FOLIO" {
			require.Equal(t, "TEMP", h[0].TemporaryShelvingLocation)
		} else {
			require.Empty(t, h[0].TemporaryShelvingLocation)
		}
	}
	// An explicitly selected generic OPAC parser keeps the old first-circulation behavior.
	h, err := NewOpacHoldingsParser(dirapi.OpacHoldingsParserConfig{}).Parse([]byte(record), LookupParams{})
	require.NoError(t, err)
	require.Len(t, h, 2)
	require.Empty(t, h[1].Location)
}
func TestProfileLookupAggregationAndFallback(t *testing.T) {
	for _, name := range []string{"Alma", "Sierra", "Koha", "FOLIO"} {
		t.Run(name, func(t *testing.T) {
			var queries []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				q := r.URL.Query().Get("x-pquery")
				queries = append(queries, q)
				fmt.Fprint(w, `<searchRetrieveResponse xmlns="http://docs.oasis-open.org/ns/search-ws/sruResponse"><version>1.1</version><numberOfRecords>4</numberOfRecords><records>`)
				for i := 0; i < 4; i++ {
					available := strings.Contains(q, "1=8")
					var record string
					if name == "Koha" {
						status := "1"
						if available {
							status = "0"
						}
						record = `<record xmlns="http://www.loc.gov/MARC21/slim"><datafield tag="952"><subfield code="b">MAIN</subfield><subfield code="7">` + status + `</subfield></datafield></record>`
					} else {
						flag, note := "0", "ON LOAN"
						if available {
							flag, note = "1", "AVAILABLE"
						}
						record = `<opacRecord><bibliographicRecord><record xmlns="http://www.loc.gov/MARC21/slim"/></bibliographicRecord><holdings><holding><localLocation>MAIN</localLocation><publicNote>` + note + `</publicNote><circulations><circulation><availableNow value="` + flag + `"/></circulation></circulations></holding></holdings></opacRecord>`
					}
					fmt.Fprintf(w, `<record><recordData>%s</recordData></record>`, record)
				}
				fmt.Fprint(w, `</records></searchRetrieveResponse>`)
			}))
			defer server.Close()
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(`{"lmsConfig":{"vendor":"`+name+`"},"catalogConfig":{"sru":{"address":"`+server.URL+`"}}}`), &entry))
			ad, err := NewLookupAdapterCreator(LookupAdapterZoom, "").GetAdapter(ill_db.Peer{CustomData: entry})
			require.NoError(t, err)
			result, err := ad.Lookup(LookupParams{Identifier: "id", Isbn: "isbn", Issn: "issn", Title: "title"})
			require.NoError(t, err)
			holdings, err := result.GetHoldings()
			require.NoError(t, err)
			require.Len(t, holdings, 4)
			require.Equal(t, []string{`@attr 1=12 "id"`, `@attr 1=7 "isbn"`, `@attr 1=8 "issn"`}, queries)
		})
	}
}
func TestProfileOnlyDoesNotEnableCatalog(t *testing.T) {
	var entry dirapi.Entry
	require.NoError(t, json.Unmarshal([]byte(`{"lmsConfig":{"vendor":"Sierra"},"catalogConfig":{"profile":"Koha"}}`), &entry))
	a, err := NewLookupAdapterCreator(LookupAdapterZoom, "").GetAdapter(ill_db.Peer{CustomData: entry})
	require.NoError(t, err)
	require.Nil(t, a)
}
