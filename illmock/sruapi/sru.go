package sruapi

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/marcxml"
	"github.com/indexdata/crosslink/sru"
	"github.com/indexdata/crosslink/sru/diag"
	"github.com/indexdata/go-utils/utils"
)

type SruApi struct {
}

func CreateSruApi() *SruApi {
	return &SruApi{}
}

// See:
// https://docs.oasis-open.org/search-ws/searchRetrieve/v1.0/os/part3-sru2.0/searchRetrieve-v1.0-os-part3-sru2.0.html#_Toc324162491
// https://github.com/indexdata/yaz/blob/master/src/srw.csv
// TODO: should have a mapping fro no to message
func getSruDiag(no string, message string, details string) *diag.Diagnostic {
	return &diag.Diagnostic{
		DiagnosticComplexType: diag.DiagnosticComplexType{
			Uri:     fmt.Sprintf("info:srw/diagnostic/1/%s", no),
			Message: message,
			Details: details,
		}}
}

func (api *SruApi) explain(w http.ResponseWriter, retVersion sru.VersionDefinition, diagnostics []diag.Diagnostic) {
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

func (api *SruApi) getIdFromQuery(query string) (string, *diag.Diagnostic) {
	var p cql.Parser
	res, err := p.Parse(query)
	if err != nil {
		return "", getSruDiag("10", "Query syntax error", err.Error())
	}
	sc := res.Clause.SearchClause
	if sc == nil {
		return "", nil
	}
	if sc.Index != "rec.id" {
		return "", getSruDiag("16", "Unsupported index", sc.Index)
	}
	if sc.Relation != cql.EQ && sc.Relation != "==" {
		return "", getSruDiag("19", "Unsupported relation", string(sc.Relation))
	}
	ids := strings.Split(sc.Term, ";")
	for _, id := range ids {
		if id == "error" {
			return "", getSruDiag("2", "System temporarily unavailable", "error")
		}
	}
	return sc.Term, nil
}

func (api *SruApi) getMarcXmlRecord(id string) (*marcxml.Record, error) {
	var record marcxml.Record

	record.Id = id
	record.Type = string(marcxml.RecordTypeTypeBibliographic)
	record.Leader = &marcxml.LeaderFieldType{Text: "00000cam a2200000 a 4500"}
	record.Controlfield = append(record.Controlfield, marcxml.ControlFieldType{Text: "123456", Id: "2", Tag: "001"})
	record.Datafield = append(record.Datafield, marcxml.DataFieldType{Tag: "245", Ind1: "1", Ind2: "0",
		Subfield: []marcxml.SubfieldatafieldType{{Code: "a", Text: "Title record from SRU mock"}}})
	localIds := strings.Split(id, ";")
	i := 1
	for _, localId := range localIds {
		if localId == "not-found" || localId == "" {
			continue
		}
		if localId == "record-error" {
			return nil, fmt.Errorf("mock record error")
		}
		var lValue string
		var sValue string
		if strings.HasPrefix(id, "return-") {
			val := strings.SplitN(strings.TrimPrefix(localId, "return-"), "::", 2)
			if len(val) < 1 || len(val[0]) < 1 {
				return nil, fmt.Errorf("invalid return- value")
			}
			if len(val) == 1 {
				sValue = val[0]
				lValue = val[0]
			}
			if len(val) == 2 {
				sValue = val[0]
				lValue = val[1]
			}
		} else {
			lValue = localId
			sValue = "ISIL:SUP" + strconv.Itoa(i)
		}
		subFields := []marcxml.SubfieldatafieldType{
			marcxml.SubfieldatafieldType{Code: "l", Text: marcxml.SubfieldDataType(lValue)},
			marcxml.SubfieldatafieldType{Code: "s", Text: marcxml.SubfieldDataType(sValue)},
		}
		record.Datafield = append(record.Datafield, marcxml.DataFieldType{Tag: "999", Ind1: "1", Ind2: "1",
			Subfield: subFields})
		i++
	}

	return &record, nil
}

func (api *SruApi) getMarcXmlBuf(id string) ([]byte, error) {
	buf, err := api.getMarcXmlRecord(id)
	if err != nil {
		return nil, err
	}
	return xml.MarshalIndent(buf, "  ", "  ")
}

func (api *SruApi) getSurrogateDiagnostic(pos uint64, errorId string, message string, details string) *sru.RecordDefinition {
	diagnostic := getSruDiag(errorId, message, details)
	buf := utils.Must(xml.MarshalIndent(diagnostic, "  ", "  "))
	var v sru.RecordXMLEscapingDefinition = sru.RecordXMLEscapingDefinitionXml
	return &sru.RecordDefinition{
		RecordSchema:      "info:srw/schema/1/diagnostics-v1.1",
		RecordXMLEscaping: &v,
		RecordPosition:    pos,
		RecordData:        sru.StringOrXmlFragmentDefinition{XMLContent: buf},
	}
}

func (api *SruApi) getMockRecords(id string, pos uint64, maximumRecords uint64) *sru.RecordsDefinition {
	records := sru.RecordsDefinition{}
	if pos != 1 || maximumRecords == 0 {
		return &records
	}
	buf, err := api.getMarcXmlBuf(id)
	var record *sru.RecordDefinition
	if err == nil {
		var v sru.RecordXMLEscapingDefinition = sru.RecordXMLEscapingDefinitionXml
		record = &sru.RecordDefinition{
			RecordSchema:      "info:srw/schema/1/marcxml-v1.1",
			RecordXMLEscaping: &v,
			RecordPosition:    pos,
			RecordData:        sru.StringOrXmlFragmentDefinition{XMLContent: buf},
		}
	} else {
		record = api.getSurrogateDiagnostic(pos, "63", "System error in retrieving records", err.Error())
	}
	if record != nil {
		records.Record = append(records.Record, *record)
	}
	return &records
}

func (api *SruApi) searchRetrieve(w http.ResponseWriter, retVersion sru.VersionDefinition, diagnostics []diag.Diagnostic, parms url.Values, query string) {
	var maximumRecords uint64 = 0
	var err error
	v := parms.Get("maximumRecords")
	if v != "" {
		maximumRecords, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			diagnostics = append(diagnostics, *getSruDiag("6", "Unsupported parameter value", "maximumRecords"))
		}
	}
	var startRecord uint64 = 1
	v = parms.Get("startRecord")
	if v != "" {
		startRecord, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			diagnostics = append(diagnostics, *getSruDiag("6", "Unsupported parameter value", "startRecord"))
		}
	}

	id, qDiag := api.getIdFromQuery(query)
	var records *sru.RecordsDefinition
	var NumberOfRecords uint64
	if qDiag != nil {
		diagnostics = append(diagnostics, *qDiag)
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
		diagnostics := []diag.Diagnostic{}
		parms := r.URL.Query()
		version := parms.Get("version")
		query := parms.Get("query")
		retVersion := sru.VersionDefinition2_0
		if version == "" || version == string(sru.VersionDefinition2_0) {
			retVersion = sru.VersionDefinition2_0
		} else {
			diagnostics = append(diagnostics, *getSruDiag("5", "Unsupported version", version))
		}
		if query == "" {
			api.explain(w, retVersion, diagnostics)
			return
		}
		api.searchRetrieve(w, retVersion, diagnostics, parms, query)
	}
}
