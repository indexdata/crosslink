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
		parms := r.URL.Query()
		version := parms.Get("version")
		query := parms.Get("query")
		fmt.Printf("SRU version: %s query: %s\n", version, query)

		retVersion := sru.VersionDefinition1_2

		sr := sru.SearchRetrieveResponse{
			SearchRetrieveResponseDefinition: sru.SearchRetrieveResponseDefinition{
				Version: &retVersion,
			},
		}

		buf := utils.Must(xml.MarshalIndent(sr, "  ", "  "))
		w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
		writeHttpResponse(w, buf)
	}
}
