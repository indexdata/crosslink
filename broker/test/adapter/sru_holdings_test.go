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

func createSruAdapter(t *testing.T, url ...string) adapter.HoldingsLookupAdapter {
	ad := adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, url)
	assert.NotNil(t, ad)
	return ad
}

func TestSru500(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("<hello"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "500")
}

func TestSruBadXml(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte("<hello"))
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "unexpected EOF")
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "unsupported RecordXMLEscapiong: string")
}

func TestSruMarcxmlUnsupportedSchema(t *testing.T) {
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "unsupported RecordSchema: info:srw/schema/1/dc-v1.1")
}

func TestSruMarcxmlBadSurrogateDiagnostic(t *testing.T) {
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
							RecordSchema:      "info:srw/schema/1/diagnostics-v1.1",
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "decoding surrogate diagnostic failed:")
}

func TestSruMarcxmlOkSurrogateDiagnostic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionXml
		var diagnostic diag.Diagnostic
		diagnostic.Uri = "info:srw/diagnostic/1/1"
		diagnostic.Message = "General system error"
		diagnostic.Details = "Something went wrong"
		diag_buf, err := xml.Marshal(diagnostic)
		assert.Nil(t, err)
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 1,
				Records: &sru.RecordsDefinition{
					Record: []sru.RecordDefinition{
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/diagnostics-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: diag_buf,
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "surrogate diagnostic: General system error: Something went wrong")
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "decoding marcxml failed")
}

func TestSruMarcxmlWithoutHoldings(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		rec_buf := marcxml.Record{RecordType: marcxml.RecordType{
			Type: "Bibliographic",
			Datafield: []marcxml.DataFieldType{
				{
					Tag: "245", // not 999
					Subfield: []marcxml.SubfieldatafieldType{
						{
							Code: "a",
							Text: "The computer",
						},
					},
				},
				{
					Tag: "999",
					// skipped as it's not 11
					Ind1: "1",
					Ind2: "0",
					Subfield: []marcxml.SubfieldatafieldType{
						{
							Code: "i",
							Text: "123",
						},
						{
							Code: "l",
							Text: "l1",
						},
						{
							Code: "s",
							Text: "s1",
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.Nil(t, err)
	assert.Len(t, holdings, 0)
}

func TestSruMarcxmlWithHoldings(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		query := r.URL.Query().Get("query")
		assert.Equal(t, "rec.id=\"123\"", query)
		w.Header().Set("Content-Type", "application/xml")
		rec_buf := marcxml.Record{RecordType: marcxml.RecordType{
			Type: "Bibliographic",
			Datafield: []marcxml.DataFieldType{
				{
					Tag:  "999",
					Ind1: "1",
					Ind2: "1",
					Subfield: []marcxml.SubfieldatafieldType{
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

	ad := createSruAdapter(t, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.Nil(t, err)
	assert.Len(t, holdings, 2)
	assert.Equal(t, "l1", holdings[0].LocalIdentifier)
	assert.Equal(t, "s1", holdings[0].Symbol)
	assert.Equal(t, "l2", holdings[1].LocalIdentifier)
	assert.Equal(t, "s2", holdings[1].Symbol)

	ad = createSruAdapter(t, server.URL, server.URL)
	p = adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err = ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.Nil(t, err)
	assert.Len(t, holdings, 4)
	assert.Equal(t, "l1", holdings[0].LocalIdentifier)
	assert.Equal(t, "s1", holdings[0].Symbol)
	assert.Equal(t, "l2", holdings[1].LocalIdentifier)
	assert.Equal(t, "s2", holdings[1].Symbol)
	assert.Equal(t, "l1", holdings[2].LocalIdentifier)
	assert.Equal(t, "s1", holdings[2].Symbol)
	assert.Equal(t, "l2", holdings[3].LocalIdentifier)
	assert.Equal(t, "s2", holdings[3].Symbol)
}
