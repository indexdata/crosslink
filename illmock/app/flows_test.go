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
	flowMessage := FlowMessage{Kind: "somekind", Timestamp: utils.XSDDateTime{Time: time.Now()}, Message: illMessage}
	api.addFlow(Flow{Message: flowMessage, Id: "rid", Role: RoleRequester, Supplier: "S1", Requester: "R1"})

	resp, err = http.Get(server.URL)
	assert.Nil(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, httpclient.ContentTypeApplicationXml, resp.Header.Get("Content-Type"))
	buf, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Nil(t, err)
	var flows1 Flows
	err = xml.Unmarshal(buf, &flows1)
	assert.Nil(t, err)
	assert.NotNil(t, flows1)
	assert.Equal(t, 1, len(flows1.Flows))
	assert.Equal(t, "rid", flows1.Flows[0].Id)
}
