package app

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestApiBadMethod(t *testing.T) {
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

func runRequest(t *testing.T, server *httptest.Server) Flows {
	resp, err := http.Get(server.URL)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Nil(t, err)
	var flows Flows
	err = xml.Unmarshal(buf, &flows)
	assert.Nil(t, err)
	assert.NotNil(t, flows)
	return flows
}

func TestGetTwoFlows(t *testing.T) {
	api := createFlowsApi()
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

	flows := runRequest(t, server)
	assert.NotNil(t, flows)
	assert.Equal(t, 0, len(flows.Flows))

	illMessage1 := iso18626.NewIso18626MessageNS()
	flowMessage := FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage1}
	flow1 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}

	//ensure order
	time.Sleep(2 * time.Millisecond)
	api.addFlow(flow1)

	flows1R := runRequest(t, server)
	assert.Len(t, flows1R.Flows, 1)
	assert.Equal(t, []Flow{flow1}, flows1R.Flows)
	assert.Len(t, flows1R.Flows[0].Message, 1)

	illMessage2 := iso18626.NewIso18626MessageNS()
	flowMessage = FlowMessage{Kind: "outgoing", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Millisecond)}, Message: *illMessage2}
	flow2 := Flow{Message: []FlowMessage{flowMessage}, Id: "rid", Role: RoleSupplier, Supplier: "S2", Requester: "R2"}
	api.addFlow(flow2)

	flows2R := runRequest(t, server)
	assert.Len(t, flows2R.Flows, 2)
	assert.Equal(t, []Flow{flow1, flow2}, flows2R.Flows)
	assert.Len(t, flows2R.Flows[0].Message, 1)
}
