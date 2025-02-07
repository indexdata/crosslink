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

func TestApiUnmarshal(t *testing.T) {
	api := createFlowsApi()
	server := httptest.NewServer(api.flowsHandler())
	defer server.Close()

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
	assert.Equal(t, 0, len(flows.Flows))

	illMessage := iso18626.Iso18626MessageNS{}
	flowMessage := FlowMessage{Kind: "incoming", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Second)}, Message: illMessage}
	flow1 := Flow{Message: flowMessage, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"}
	api.addFlow(flow1)

	resp, err = http.Get(server.URL)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Nil(t, err)
	var flows1R Flows
	err = xml.Unmarshal(buf, &flows1R)
	assert.Nil(t, err)
	assert.Equal(t, []Flow{flow1}, flows1R.Flows)

	flowMessage = FlowMessage{Kind: "outgoing", Timestamp: utils.XSDDateTime{Time: time.Now().UTC().Round(time.Second)}, Message: illMessage}
	flow2 := Flow{Message: flowMessage, Id: "rid", Role: RoleSupplier, Supplier: "S2", Requester: "R2"}
	api.addFlow(flow2)

	resp, err = http.Get(server.URL)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Nil(t, err)
	var flows2R Flows
	err = xml.Unmarshal(buf, &flows2R)
	assert.Nil(t, err)
	assert.Equal(t, []Flow{flow1, flow2}, flows2R.Flows)
}
