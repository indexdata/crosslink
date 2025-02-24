package adapter

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/marcxml"
	"github.com/indexdata/crosslink/sru"
	"github.com/indexdata/crosslink/sru/diag"
	"github.com/stretchr/testify/assert"
)

func TestSru500(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("<hello"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "500")
}

func TestSruBadXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte("<hello"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "decoding failed")
}

func TestSruBadDiagnostics(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		retVersion := sru.VersionDefinition2_0
		diagnostics := []diag.Diagnostic{
			{
				DiagnosticComplexType: diag.DiagnosticComplexType{
					Uri:     "info:srw/diagnostic/1/1",
					Message: "General system error",
					Details: "Something went wrong",
				},
			},
		}
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:     &retVersion,
				Diagnostics: &sru.DiagnosticsDefinition{Diagnostic: diagnostics},
			},
		}
		buf, _ := xml.Marshal(sr)
		w.Write(buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "General system error: Something went wrong")
}

func TestSruMarcxmlNoHits(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		retVersion := sru.VersionDefinition2_0
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 0,
			},
		}
		buf, _ := xml.Marshal(sr)
		w.Write(buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, err := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, holdings, 0)
}

func TestSruMarcxmlStringEncoding(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionString
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 1,
				Records: &sru.RecordsDefinition{
					Record: []sru.RecordDefinition{
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: []byte("<record></record>"),
							},
						},
					},
				},
			},
		}
		sru_buf, _ := xml.Marshal(sr)
		w.Write(sru_buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, err := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, holdings, 0)
}

func TestSruMarcxmlInvalidSchema(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionXml
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 1,
				Records: &sru.RecordsDefinition{
					Record: []sru.RecordDefinition{
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/dc-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: []byte("<record></record>"),
							},
						},
					},
				},
			},
		}
		sru_buf, _ := xml.Marshal(sr)
		w.Write(sru_buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, err := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, holdings, 0)
}

func TestSruMarcxmlBadMarc(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		rec_buf := []byte("<rec")
		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionXml
		sru_buf, _ := xml.Marshal(rec_buf)
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 1,
				Records: &sru.RecordsDefinition{
					Record: []sru.RecordDefinition{
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: sru_buf,
							},
						},
					},
				},
			},
		}
		sru_buf, _ = xml.Marshal(sr)
		w.Write(sru_buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, err := ad.Lookup(p)
	assert.ErrorContains(t, err, "decoding marcxml failed")
}

func TestSruMarcxmlWithHoldings(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		rec_buf := marcxml.Record{RecordType: marcxml.RecordType{
			Type: "Bibliographic",
			Datafield: []marcxml.DataFieldType{
				{
					Tag: "999",
					Subfield: []marcxml.SubfieldatafieldType{
						{
							Code: "i", // cluster identifier
							Text: "123",
						},
						{
							Code: "l", // local identifier
							Text: "l1",
						},
						{
							Code: "s", // source identifier
							Text: "s1",
						},
						{
							Code: "l",
							Text: "l2",
						},
						{
							Code: "s",
							Text: "s2",
						},
					},
				},
			},
		}}
		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionXml
		sru_buf, _ := xml.Marshal(rec_buf)
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 1,
				Records: &sru.RecordsDefinition{
					Record: []sru.RecordDefinition{
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: sru_buf,
							},
						},
					},
				},
			},
		}
		sru_buf, _ = xml.Marshal(sr)
		w.Write(sru_buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var ad adapter.HoldingsLookupAdapter = adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, err := ad.Lookup(p)
	assert.Nil(t, err)
	assert.Len(t, holdings, 2)
	assert.Equal(t, "l1", holdings[0].LocalIdentifier)
	assert.Equal(t, "s1", holdings[0].Symbol)
	assert.Equal(t, "l2", holdings[1].LocalIdentifier)
	assert.Equal(t, "s2", holdings[1].Symbol)
}
