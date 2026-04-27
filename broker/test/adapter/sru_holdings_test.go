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

func createSruAdapter(t *testing.T, isxn bool, url ...string) adapter.HoldingsLookupAdapter {
	ad := adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, url, isxn)
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

	ad := createSruAdapter(t, false, server.URL)
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

	ad := createSruAdapter(t, false, server.URL)
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

	ad := createSruAdapter(t, false, server.URL)
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

	ad := createSruAdapter(t, false, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.NoError(t, err)
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

	ad := createSruAdapter(t, false, server.URL)
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

	ad := createSruAdapter(t, false, server.URL)
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

	ad := createSruAdapter(t, false, server.URL)
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
		assert.NoError(t, err)
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

	ad := createSruAdapter(t, false, server.URL)
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

	ad := createSruAdapter(t, false, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	_, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.ErrorContains(t, err, "decoding marcxml failed")
}

func TestSruMarcxmlWithFallbackHoldings(t *testing.T) {
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
					Tag:  "999",
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

	ad := createSruAdapter(t, false, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	assert.Equal(t, "l1", holdings[0].LocalIdentifier)
	assert.Equal(t, "s1", holdings[0].Symbol)
}

func TestSruMarcxmlWithHoldingsDoesNotUseFallback(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		recBuf := marcxml.Record{RecordType: marcxml.RecordType{
			Type: "Bibliographic",
			Datafield: []marcxml.DataFieldType{
				{
					Tag:  "999",
					Ind1: "1",
					Ind2: "1",
					Subfield: []marcxml.SubfieldatafieldType{
						{
							Code: "l",
							Text: "primary-local",
						},
						{
							Code: "s",
							Text: "primary-symbol",
						},
					},
				},
				{
					Tag:  "999",
					Ind1: "1",
					Ind2: "0",
					Subfield: []marcxml.SubfieldatafieldType{
						{
							Code: "l",
							Text: "fallback-local",
						},
						{
							Code: "s",
							Text: "fallback-symbol",
						},
					},
				},
			},
		}}
		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionXml
		recordXML, _ := xml.Marshal(recBuf)
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
								XMLContent: recordXML,
							},
						},
					},
				},
			},
		}
		responseXML, _ := xml.Marshal(sr)
		w.Write(responseXML)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createSruAdapter(t, false, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NotEmpty(t, query)
	assert.NoError(t, err)
	assert.Len(t, holdings, 1)
	assert.Equal(t, "primary-local", holdings[0].LocalIdentifier)
	assert.Equal(t, "primary-symbol", holdings[0].Symbol)
}

func TestSruMarcxmlWithHoldings(t *testing.T) {
	var receivedQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		query := r.URL.Query().Get("query")
		receivedQuery = query
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
		xml_buf1, err := xml.Marshal(rec_buf)
		assert.NoError(t, err)

		rec_buf = marcxml.Record{RecordType: marcxml.RecordType{
			Type: "Bibliographic",
			Datafield: []marcxml.DataFieldType{
				{
					Tag:  "999",
					Ind1: "1",
					Ind2: "1",
					Subfield: []marcxml.SubfieldatafieldType{
						{
							Code: "l", // local identifier
							Text: "l3",
						},
						{
							Code: "s", // source identifier
							Text: "s3",
						},
					},
				},
			},
		}}
		xml_buf2, err := xml.Marshal(rec_buf)
		assert.NoError(t, err)

		retVersion := sru.VersionDefinition2_0
		escaping := sru.RecordXMLEscapingDefinitionXml
		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:         &retVersion,
				NumberOfRecords: 2,
				Records: &sru.RecordsDefinition{
					Record: []sru.RecordDefinition{
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: xml_buf1,
							},
						},
						{
							RecordXMLEscaping: &escaping,
							RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
							RecordData: sru.StringOrXmlFragmentDefinition{
								XMLContent: xml_buf2,
							},
						},
					},
				},
			},
		}
		xml_buf, err := xml.Marshal(sr)
		assert.NoError(t, err)
		w.Write(xml_buf)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ad := createSruAdapter(t, true, server.URL)
	p := adapter.HoldingLookupParams{
		Identifier: "123",
	}
	holdings, query, err := ad.Lookup(p)
	assert.NoError(t, err)
	assert.NotEmpty(t, query)
	assert.Equal(t, "rec.id = 123", receivedQuery)
	assert.Len(t, holdings, 3)
	assert.Equal(t, "l1", holdings[0].LocalIdentifier)
	assert.Equal(t, "s1", holdings[0].Symbol)
	assert.Equal(t, "l2", holdings[1].LocalIdentifier)
	assert.Equal(t, "s2", holdings[1].Symbol)
	assert.Equal(t, "l3", holdings[2].LocalIdentifier)
	assert.Equal(t, "s3", holdings[2].Symbol)

	ad = createSruAdapter(t, true, server.URL, server.URL)
	p = adapter.HoldingLookupParams{
		Identifier: "123",
		Isbn:       "99-222",
		Issn:       "99-333",
	}
	holdings, query, err = ad.Lookup(p)
	assert.NoError(t, err)
	assert.NotEmpty(t, query)
	assert.Equal(t, "rec.id = 123 or isbn = 99-222 or issn = 99-333", receivedQuery)
	assert.Len(t, holdings, 6)
	assert.Equal(t, "l1", holdings[0].LocalIdentifier)
	assert.Equal(t, "s1", holdings[0].Symbol)
	assert.Equal(t, "l2", holdings[1].LocalIdentifier)
	assert.Equal(t, "s2", holdings[1].Symbol)
	assert.Equal(t, "l3", holdings[2].LocalIdentifier)
	assert.Equal(t, "s3", holdings[2].Symbol)

	assert.Equal(t, "l1", holdings[3].LocalIdentifier)
	assert.Equal(t, "s1", holdings[3].Symbol)
	assert.Equal(t, "l2", holdings[4].LocalIdentifier)
	assert.Equal(t, "s2", holdings[4].Symbol)
	assert.Equal(t, "l3", holdings[5].LocalIdentifier)
	assert.Equal(t, "s3", holdings[5].Symbol)

	ad = createSruAdapter(t, false, server.URL)
	p = adapter.HoldingLookupParams{
		Isbn: "99-222",
	}
	_, _, err = ad.Lookup(p)
	assert.Error(t, err)
	assert.Equal(t, "no search parameters provided for SRU lookup", err.Error())
}
