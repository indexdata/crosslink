package app

import (
	"encoding/xml"
	"net/http"
	"slices"
	"sync"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type FlowsApi struct {
	flows sync.Map
}

type FlowMessage struct {
	Kind      string                     `xml:"kind,attr"`
	Timestamp utils.XSDDateTime          `xml:"timestamp,attr"`
	Message   iso18626.Iso18626MessageNS `xml:"ISO18626Message"`
}

type FlowError struct {
	Kind    string `xml:"kind,attr"`
	Message string `xml:"message"`
}

type Flow struct {
	Id        string        `xml:"id,attr"`
	Role      Role          `xml:"role,attr"`
	Supplier  string        `xml:"supplier,attr"`
	Requester string        `xml:"requester,attr"`
	Message   []FlowMessage `xml:"message,omitempty"`
	Error     *FlowError    `xml:"error,omitempty"`
}

type Flows struct {
	XMLName xml.Name `xml:"flows"`
	Flows   []Flow   `xml:"flow"`
}

func createFlowsApi() *FlowsApi {
	api := &FlowsApi{}
	api.init()
	return api
}

func (api *FlowsApi) init() {
	api.flows.Clear()
}

func cmpFlow(i, j Flow) int {
	// there may be multiple timestamps in the message, but we only care about the first one
	i_empty := len(i.Message) == 0
	j_empty := len(j.Message) == 0
	if !i_empty && !j_empty {
		if x := i.Message[0].Timestamp.After(j.Message[0].Timestamp.Time); x {
			return 1
		} else if x := i.Message[0].Timestamp.Before(j.Message[0].Timestamp.Time); x {
			return -1
		}
		return 0
	}
	if !i_empty {
		return 1
	}
	if !j_empty {
		return -1
	}
	return 0
}

func (api *FlowsApi) flowsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		flowsList := Flows{}
		api.flows.Range(func(key, value interface{}) bool {
			flow := value.(Flow)
			// TODO filter the list of flows
			flowsList.Flows = append(flowsList.Flows, flow)
			return true
		})
		slices.SortFunc(flowsList.Flows, cmpFlow)
		// flowsList is not a pointer so MarshalIndent will always work
		buf := utils.Must(xml.MarshalIndent(flowsList, "  ", "  "))
		w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
		writeHttpResponse(w, buf)
	}
}

func (api *FlowsApi) addFlow(flow Flow) {
	key := string(flow.Role) + "/" + flow.Id
	v, ok := api.flows.Load(key)
	if ok {
		eFlow := v.(Flow)
		eFlow.Message = append(eFlow.Message, flow.Message...)
	} else {
		api.flows.Store(key, flow)
	}
}
