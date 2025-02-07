package app

import (
	"encoding/xml"
	"net/http"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type FlowsApi struct {
	flowsList Flows
}

type FlowMessage struct {
	XMLName   xml.Name          `xml:"message"`
	Kind      string            `xml:"kind,attr"`
	Timestamp utils.XSDDateTime `xml:"timestamp,attr"`
	Message   iso18626.Iso18626MessageNS
}

type Flow struct {
	XMLName   xml.Name `xml:"flow"`
	Id        string   `xml:"id,attr"`
	Role      Role     `xml:"role,attr"`
	Supplier  string   `xml:"supplier,attr"`
	Requester string   `xml:"requester,attr"`
	Message   FlowMessage
}

type Flows struct {
	XMLName xml.Name `xml:"flows"`
	Flows   []Flow
}

func createFlowsApi() *FlowsApi {
	api := &FlowsApi{}
	api.init()
	return api
}

func (api *FlowsApi) init() {
	api.flowsList.Flows = make([]Flow, 0)
}

func (api *FlowsApi) flowsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
		// api.flowsList is not a pointer so MarshalIndent will always work
		buf := utils.Must(xml.MarshalIndent(api.flowsList, "  ", "  "))
		writeHttpResponse(w, buf)
	}
}

func (FlowsApi *FlowsApi) addFlow(flow Flow) {
	FlowsApi.flowsList.Flows = append(FlowsApi.flowsList.Flows, flow)
}
