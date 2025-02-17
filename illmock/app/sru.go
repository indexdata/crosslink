package app

import (
	"encoding/xml"
	"fmt"
	"net/http"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/sru"
	"github.com/indexdata/go-utils/utils"
)

type SruApi struct {
}

func createSruApi() *SruApi {
	api := &SruApi{}
	return api
}

func (api *SruApi) explain(w http.ResponseWriter, retVersion sru.VersionDefinition, diagnostics []sru.Diagnostic) {
	er := sru.ExplainResponse{
		ExplainResponseDefinition: sru.ExplainResponseDefinition{
			Version:     &retVersion,
			Diagnostics: &sru.DiagnosticsDefinition{Diagnostic: diagnostics},
		},
	}
	buf := utils.Must(xml.MarshalIndent(er, "  ", "  "))
	w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
	writeHttpResponse(w, buf)
}

// TODO: use cql-go for parsing the query
func (api *SruApi) getIdFromQuery(query string) (string, error) {
	var id string
	n, err := fmt.Sscanf(query, "id=%s", &id)
	if err != nil || n != 1 {
		return "", fmt.Errorf("syntax error")
	}
	return id, nil
}

func (api *SruApi) getMockRecords(id string, pos uint64) *sru.RecordsDefinition {
	records := sru.RecordsDefinition{}
	record := sru.RecordDefinition{
		RecordPacking:  "xml",
		RecordPosition: pos,
		RecordData:     sru.StringOrXmlFragmentDefinition{StringOrXmlFragmentDefinition: []byte("<record><id>" + id + "</id><title>Mock record</title></record>")},
	}
	records.Record = append(records.Record, record)
	return &records
}

func (api *SruApi) searchRetrieve(w http.ResponseWriter, retVersion sru.VersionDefinition, diagnostics []sru.Diagnostic, query string) {
	id, err := api.getIdFromQuery(query)
	var records *sru.RecordsDefinition
	var NumberOfRecords uint64
	if err != nil {
		diagnostics = append(diagnostics, sru.Diagnostic{
			DiagnosticComplexType: sru.DiagnosticComplexType{
				Uri:     "info:srw/diagnostic/1/10",
				Message: "Query syntax error",
				Details: err.Error(),
			}})
	} else {
		records = api.getMockRecords(id, 1)
		NumberOfRecords = 1
	}
	sr := sru.SearchRetrieveResponse{
		SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
			Version:         &retVersion,
			Diagnostics:     &sru.DiagnosticsDefinition{Diagnostic: diagnostics},
			Records:         records,
			NumberOfRecords: NumberOfRecords,
		},
	}

	buf := utils.Must(xml.MarshalIndent(sr, "  ", "  "))
	w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
	writeHttpResponse(w, buf)
}

func (api *SruApi) sruHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		diagnostics := []sru.Diagnostic{}
		parms := r.URL.Query()
		version := parms.Get("version")
		query := parms.Get("query")
		retVersion := sru.VersionDefinition2_0
		fmt.Printf("SRU version: %s query: %s\n", version, query)
		if version == "" || version == string(sru.VersionDefinition2_0) {
			retVersion = sru.VersionDefinition2_0
		} else if version == string(sru.VersionDefinition1_2) {
			retVersion = sru.VersionDefinition1_2
		} else {
			diagnostics = append(diagnostics, sru.Diagnostic{
				DiagnosticComplexType: sru.DiagnosticComplexType{
					Uri:     "info:srw/diagnostic/1/5",
					Message: "Unsupported version",
					Details: "2.0",
				}})
		}
		if query == "" {
			api.explain(w, retVersion, diagnostics)
		} else {
			api.searchRetrieve(w, retVersion, diagnostics, query)
		}
	}
}
