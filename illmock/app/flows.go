package app

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type FlowsApi struct {
	flows         sync.Map
	cleanInterval time.Duration
	cleanTimeout  time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
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
	api.cleanInterval = 1 * time.Minute
	api.cleanTimeout = 5 * time.Minute
	api.ctx, api.cancel = context.WithCancel(context.Background())
}

func (api *FlowsApi) ParseEnv() error {
	v := utils.GetEnv("CLEAN_TIMEOUT", "10m")
	if v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d == 0 {
			return errors.New("CLEAN_TIMEOUT must be greater than 0")
		}
		api.cleanTimeout = d
		api.cleanInterval = d / 10
	}
	return nil
}

func (api *FlowsApi) Run() {
	go api.cleaner()
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

func (api *FlowsApi) cleaner() {
	ticker := time.NewTicker(api.cleanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			api.clean()
		case <-api.ctx.Done():
			return
		}
	}
}

func (api *FlowsApi) clean() {
	api.flows.Range(func(key, value interface{}) bool {
		flow := value.(*Flow)
		if time.Since(flow.Modified) > api.cleanTimeout {
			api.flows.Delete(key)
		}
		return true
	})
}

func (api *FlowsApi) Shutdown() {
	api.cancel()
	api.flows.Clear()
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
