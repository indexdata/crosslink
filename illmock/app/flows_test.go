package app

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"
)

func TestApiNoInit(t *testing.T) {
	api := &FlowsApi{}
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

	resp, err := http.Get(server.URL)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestFlowApiBadMethod(t *testing.T) {
	api := createFlowsApi()
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

	resp, err := http.Post(server.URL, "text/plain", strings.NewReader("hello"))
	assert.Nil(t, err)
	assert.Equal(t, 405, resp.StatusCode)
}

func TestMarshalRequest(t *testing.T) {
	illMessage := iso18626.NewIso18626MessageNS()
	illMessage.Request = &iso18626.Request{}
	illMessage.Request.Header.RequestingAgencyRequestId = "rid"
	flowMessage := FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage}
	flow := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	buf, err := xml.MarshalIndent(&flow, "  ", "  ")
	assert.Nil(t, err)
	assert.Contains(t, string(buf), "xmlns=\"http://illtransactions.org/2013/iso18626\"")         // namespace declaration
	assert.Contains(t, string(buf), "<requestingAgencyRequestId>rid</requestingAgencyRequestId>") // part of request XML
	log.Info(string(buf))
	var flowR Flow
	err = xml.Unmarshal(buf, &flowR)
	assert.Nil(t, err)
	assert.Nil(t, flowR.Error)
	assert.NotNil(t, flowR.Message)
	assert.Len(t, flowR.Message, 1)
	assert.NotNil(t, flowR.Message[0].Message.Request)
	assert.Equal(t, flowR.Message[0].Message.Request.Header.RequestingAgencyRequestId, "rid")
}

func TestMarshalError(t *testing.T) {
	flowError := &FlowError{Message: "error message", Kind: "outgoing-error"}
	flow := Flow{Error: flowError, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	buf, err := xml.MarshalIndent(&flow, "  ", "  ")
	assert.Nil(t, err)
	log.Info(string(buf))
	var flowR Flow
	err = xml.Unmarshal(buf, &flowR)
	assert.Nil(t, err)
	assert.Nil(t, flowR.Message)
	assert.NotNil(t, flowR.Error)
	assert.Equal(t, flow, flowR)
}

func TestCmpFlow(t *testing.T) {
	t1 := time.Now()
	t2 := time.Now().Add(time.Duration(1))
	flow1 := Flow{Message: []FlowMessage{}, Modified: t1}

	flow2 := Flow{Message: []FlowMessage{}, Modified: t2}

	assert.Equal(t, 0, cmpFlow(flow1, flow1))

	assert.Equal(t, -1, cmpFlow(flow1, flow2))
	assert.Equal(t, 1, cmpFlow(flow2, flow1))
}

func runRequest(t *testing.T, server *httptest.Server, params string) Flows {
	resp, err := http.Get(server.URL + params)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	assert.Nil(t, err)
	var flows Flows
	err = xml.Unmarshal(buf, &flows)
	assert.Nil(t, err)
	assert.NotNil(t, flows)
	return flows
}

func TestGetFlows(t *testing.T) {
	api := createFlowsApi()
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

	flows := runRequest(t, server, "")
	assert.NotNil(t, flows)
	assert.Equal(t, 0, len(flows.Flows))

	illMessage1 := iso18626.NewIso18626MessageNS()
	flowMessage := FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage1}
	flow1 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	api.addFlow(flow1)

	flowsR := runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 1)
	assert.Equal(t, []Flow{flow1}, flowsR.Flows)
	assert.Len(t, flowsR.Flows[0].Message, 1)

	illMessage2 := iso18626.NewIso18626MessageNS()
	flowMessage = FlowMessage{Kind: "outgoing", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage2}
	flow2 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleSupplier, Supplier: "S2", Requester: "R2"}
	api.addFlow(flow2)

	flowsR = runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 2)
	assert.Equal(t, []Flow{flow1, flow2}, flowsR.Flows)
	assert.Len(t, flowsR.Flows[0].Message, 1)

	illMessage3 := iso18626.NewIso18626MessageNS()
	flowMessage = FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage3}
	flow3 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid2", Role: RoleSupplier, Supplier: "S3", Requester: "R3"}
	api.addFlow(flow3)

	flowsR = runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 3)
	assert.Equal(t, []Flow{flow1, flow2, flow3}, flowsR.Flows)

	flowsR = runRequest(t, server, "?id=rid")
	assert.Equal(t, []Flow{flow1, flow2}, flowsR.Flows)

	flowsR = runRequest(t, server, "?id=rid2")
	assert.Equal(t, []Flow{flow3}, flowsR.Flows)

	flowsR = runRequest(t, server, "?role=requester")
	assert.Equal(t, []Flow{flow1}, flowsR.Flows)

	flowsR = runRequest(t, server, "?role=supplier")
	assert.Equal(t, []Flow{flow2, flow3}, flowsR.Flows)

	flowsR = runRequest(t, server, "?supplier=S1")
	assert.Equal(t, []Flow{flow1}, flowsR.Flows)

	flowsR = runRequest(t, server, "?requester=R2")
	assert.Equal(t, []Flow{flow2}, flowsR.Flows)

	flowsR = runRequest(t, server, "?requester=other")
	assert.Equal(t, []Flow(nil), flowsR.Flows)

	// merged with flow1
	illMessage4 := iso18626.NewIso18626MessageNS()
	flowMessage = FlowMessage{Kind: "outgoing", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage4}
	flow4 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	api.addFlow(flow4)

	flowsR = runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 3)
	assert.Equal(t, flow2, flowsR.Flows[0])
	assert.Equal(t, flow3, flowsR.Flows[1])

	flow1.Message = append(flow1.Message, flowMessage) // merge flow4 into flow1
	assert.Equal(t, []Flow{flow2, flow3, flow1}, flowsR.Flows)
	assert.Len(t, flowsR.Flows[2].Message, 2)
}

func TestCleanerExpire(t *testing.T) {
	api := createFlowsApi()
	api.cleanTimeout = 1 * time.Microsecond
	api.cleanInterval = 1 * time.Millisecond
	api.Run()
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

	illMessage1 := iso18626.NewIso18626MessageNS()
	flowMessage := FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage1}
	flow1 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	api.addFlow(flow1)

	flowsR := runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 1)

	time.Sleep(2 * time.Millisecond)
	flowsR = runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 0)

	api.Shutdown()

	time.Sleep(1 * time.Millisecond)

	api.addFlow(flow1)

	time.Sleep(1 * time.Millisecond)

	flowsR = runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 1)
}

func TestCleanerKeep(t *testing.T) {
	api := createFlowsApi()
	api.cleanInterval = 1 * time.Millisecond
	api.cleanTimeout = 1 * time.Second
	api.Run()
	defer api.Shutdown()
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

	illMessage1 := iso18626.NewIso18626MessageNS()
	flowMessage := FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage1}
	flow1 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	api.addFlow(flow1)

	flowsR := runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 1)

	time.Sleep(2 * time.Millisecond)
	flowsR = runRequest(t, server, "")
	assert.Len(t, flowsR.Flows, 1)
}

func TestFlowsParseEnv(t *testing.T) {
	os.Setenv("CLEAN_TIMEOUT", "8m")
	api := createFlowsApi()
	err := api.ParseEnv()
	assert.Nil(t, err)
	assert.Equal(t, "8m0s", api.cleanTimeout.String())
	assert.Equal(t, "48s", api.cleanInterval.String())

	os.Setenv("CLEAN_TIMEOUT", "x")
	err = api.ParseEnv()
	assert.NotNil(t, err)

	os.Setenv("CLEAN_TIMEOUT", "0")
	err = api.ParseEnv()
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "CLEAN_TIMEOUT must be greater than 0")
}
