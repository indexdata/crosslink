package sruapi

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/marcxml"
	"github.com/indexdata/crosslink/sru"
	"github.com/indexdata/go-utils/utils"
)

type SruApi struct {
}

func CreateSruApi() *SruApi {
	return &SruApi{}
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
	netutil.WriteHttpResponse(w, buf)
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

func (api *SruApi) getMarcXmlRecord(id string) *marcxml.Record {
	var record marcxml.Record

	record.Id = id
	record.Type = string(marcxml.RecordTypeTypeBibliographic)
	record.Leader = &marcxml.LeaderFieldType{Text: "00000cam a2200000 a 4500"}
	record.Controlfield = append(record.Controlfield, marcxml.ControlFieldType{Text: "123456", Id: "2", Tag: "001"})
	record.Datafield = append(record.Datafield, marcxml.DataFieldType{Tag: "245", Ind1: "1", Ind2: "0",
		Subfield: []marcxml.SubfieldatafieldType{{Code: "a", Text: "Mock record from SRU"}}})
	subFields := []marcxml.SubfieldatafieldType{{Code: "i", Text: marcxml.SubfieldDataType(id)}}
	localIds := strings.Split(id, ";")
	for _, localId := range localIds {
		subFields = append(subFields, marcxml.SubfieldatafieldType{Code: "l", Text: marcxml.SubfieldDataType(localId)})
		subFields = append(subFields, marcxml.SubfieldatafieldType{Code: "s", Text: marcxml.SubfieldDataType(localId)})
	}
	record.Datafield = append(record.Datafield, marcxml.DataFieldType{Tag: "999", Ind1: "1", Ind2: "0",
		Subfield: subFields})
	return &record
}

func (api *SruApi) getMarcBuf(id string) ([]byte, error) {
	if id == "sd" {
		return nil, fmt.Errorf("mock error")
	}
	return xml.MarshalIndent(api.getMarcXmlRecord(id), "  ", "  ")
}

func (api *SruApi) produceSurrogateDiagnostic(pos uint64, message string) *sru.RecordDefinition {
	diagnostic := sru.Diagnostic{
		DiagnosticComplexType: sru.DiagnosticComplexType{
			Uri:     "info:srw/diagnostic/1/63",
			Message: message,
		},
	}
	buf := utils.Must(xml.MarshalIndent(diagnostic, "  ", "  "))
	var v sru.RecordXMLEscapingDefinition = sru.RecordXMLEscapingDefinitionXml
	return &sru.RecordDefinition{
		RecordSchema:      "info::srw/schema/1/diagnostics-v1.1",
		RecordXMLEscaping: &v,
		RecordPosition:    pos,
		RecordData:        sru.StringOrXmlFragmentDefinition{StringOrXmlFragmentDefinition: buf},
	}
}

func (api *SruApi) getMockRecords(id string, pos uint64, maximumRecords uint64) *sru.RecordsDefinition {
	records := sru.RecordsDefinition{}
	if pos != 1 || maximumRecords == 0 {
		return &records
	}
	buf, err := api.getMarcBuf(id)
	var record *sru.RecordDefinition
	if err == nil {
		var v sru.RecordXMLEscapingDefinition = sru.RecordXMLEscapingDefinitionXml
		record = &sru.RecordDefinition{
			RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
			RecordXMLEscaping: &v,
			RecordPosition:    pos,
			RecordData:        sru.StringOrXmlFragmentDefinition{StringOrXmlFragmentDefinition: buf},
		}
	} else {
		record = api.produceSurrogateDiagnostic(pos, err.Error())
	}
	if record != nil {
		records.Record = append(records.Record, *record)
	}
	return &records
}

func (api *SruApi) searchRetrieve(w http.ResponseWriter, retVersion sru.VersionDefinition, diagnostics []sru.Diagnostic, parms url.Values, query string) {
	var maximumRecords uint64 = 0
	var err error
	v := parms.Get("maximumRecords")
	if v != "" {
		maximumRecords, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			diagnostics = append(diagnostics, sru.Diagnostic{
				DiagnosticComplexType: sru.DiagnosticComplexType{
					Uri:     "info:srw/diagnostic/1/6", // Unsupported parameter value
					Message: "maximumRecords",
					Details: err.Error(),
				}})
		}
	}
	var startRecord uint64 = 1
	v = parms.Get("startRecord")
	if v != "" {
		startRecord, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			diagnostics = append(diagnostics, sru.Diagnostic{
				DiagnosticComplexType: sru.DiagnosticComplexType{
					Uri:     "info:srw/diagnostic/1/6", // Unsupported parameter value
					Message: "startRecord",
					Details: err.Error(),
				}})
		}
	}

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
		records = api.getMockRecords(id, startRecord, maximumRecords)
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
	netutil.WriteHttpResponse(w, buf)
}

func (api *SruApi) HttpHandler() http.HandlerFunc {
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
			return
		}
		api.searchRetrieve(w, retVersion, diagnostics, parms, query)
	}
}
