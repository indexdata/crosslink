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
			er := sru.ExplainResponse{
				ExplainResponseDefinition: sru.ExplainResponseDefinition{
					Version:     &retVersion,
					Diagnostics: &sru.DiagnosticsDefinition{Diagnostic: diagnostics},
				},
			}
			buf := utils.Must(xml.MarshalIndent(er, "  ", "  "))
			w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
			writeHttpResponse(w, buf)
			return
		}

		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version:     &retVersion,
				Diagnostics: &sru.DiagnosticsDefinition{Diagnostic: diagnostics},
			},
		}
		buf := utils.Must(xml.MarshalIndent(sr, "  ", "  "))
		w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
		writeHttpResponse(w, buf)
	}
}
