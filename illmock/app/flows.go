package app

import (
	"encoding/xml"
	"net/http"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/iso18626"
)

type FlowsApi struct {
	flowsList Flows
}

type Flow struct {
	XMLName xml.Name                   `xml:"flow"`
	Message iso18626.Iso18626MessageNS `xml:"message"`
}

type Flows struct {
	XMLName xml.Name `xml:"flows"`
	Flows   []Flow
}

func createFlowsApi() *FlowsApi {
	api := &FlowsApi{}
	api.flowsList.Flows = make([]Flow, 0)
	return api
}

func (api *FlowsApi) flowsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
		buf, err := xml.MarshalIndent(api.flowsList.Flows, "  ", "  ")
		if err != nil {
			http.Error(w, "failed to marshal flows", http.StatusInternalServerError)
			return
		}
		writeHttpResponse(w, buf)
	}
}

func (FlowsApi *FlowsApi) addFlow(flow Flow) {
	FlowsApi.flowsList.Flows = append(FlowsApi.flowsList.Flows, flow)
}
