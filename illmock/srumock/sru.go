package srumock

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/crosslink/sru"
	"github.com/indexdata/go-utils/utils"
)

type SruApi struct {
}

var log *slog.Logger = slogwrap.SlogWrap()

func createSruApi() *SruApi {
	api := &SruApi{}
	return api
}

func writeHttpResponse(w http.ResponseWriter, buf []byte) {
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buf)
	if err != nil {
		log.Warn("writeResponse", "error", err.Error())
	}
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

func (api *SruApi) getIdFromQuery(query string) (string, error) {
	var p cql.Parser
	res, err := p.Parse(query)
	if err != nil {
		return "", err
	}
	sc := res.Clause.SearchClause
	if sc == nil {
		return "", fmt.Errorf("missing search clause")
	}
	if sc.Index != "id" {
		return "", fmt.Errorf("unknown index: %s", sc.Index)
	}
	if sc.Relation != cql.EQ && sc.Relation != "==" {
		return "", fmt.Errorf("unsupported relation: %s", sc.Relation)
	}
	return sc.Term, nil
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
