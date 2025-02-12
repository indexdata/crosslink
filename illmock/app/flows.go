package app

import (
	"encoding/xml"
	"net/http"
	"slices"
	"sync"
	"time"

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
	Modified  time.Time     `xml:"-"`
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
	if i.Modified.After(j.Modified) {
		return 1
	}
	if i.Modified.Before(j.Modified) {
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
		parms := r.URL.Query()
		role := parms.Get("role")
		supplier := parms.Get("supplier")
		requester := parms.Get("requester")
		id := parms.Get("id")

		var flowsList Flows
		api.flows.Range(func(key, value interface{}) bool {
			flow := value.(*Flow)
			if role != "" && role != string(flow.Role) {
				return true
			}
			if supplier != "" && supplier != flow.Supplier {
				return true
			}
			if requester != "" && requester != flow.Requester {
				return true
			}
			if id != "" && id != flow.Id {
				return true
			}
			flowsList.Flows = append(flowsList.Flows, *flow)
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
		eFlow := v.(*Flow)
		eFlow.Modified = time.Now()
		eFlow.Message = append(eFlow.Message, flow.Message...)
	} else {
		flow.Modified = time.Now()
		api.flows.Store(key, &flow)
	}
}
