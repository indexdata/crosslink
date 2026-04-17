package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/stretchr/testify/assert"
)

func TestGetSymbolForRequest(t *testing.T) {
	req, _ := http.NewRequest("GET", "/broker/patron_request", strings.NewReader("{"))
	req.RequestURI = "/broker/patron_request"
	tenant := "req"
	resolved, err := GetSymbolForRequest(req, common.NewTenant("ISIL:{tenant}"), &tenant, nil)
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:REQ", resolved)

	resolved, err = GetSymbolForRequest(req, common.NewTenant("ISIL:{tenant}"), nil, nil)
	assert.Equal(t, "X-Okapi-Tenant must be specified", err.Error())
	assert.Equal(t, "", resolved)

	resolved, err = GetSymbolForRequest(req, common.NewTenant(""), &tenant, nil)
	assert.Equal(t, "tenant mapping must be specified", err.Error())
	assert.Equal(t, "", resolved)
}

func TestWithBrokerPrefix(t *testing.T) {
	brokerReq, _ := http.NewRequest("GET", "/broker/patron_request", strings.NewReader("{"))
	brokerReq.RequestURI = "/broker/patron_request"
	assert.True(t, IsBrokerRequest(brokerReq))
	assert.Equal(t, "/broker/patron_requests/1", WithBrokerPrefix(brokerReq, "/patron_requests/1"))
	assert.Equal(t, "/broker/", WithBrokerPrefix(brokerReq, ""))
	assert.Equal(t, "/broker/", WithBrokerPrefix(brokerReq, "/"))

	regularReq, _ := http.NewRequest("GET", "/patron_request", strings.NewReader("{"))
	regularReq.RequestURI = "/patron_request"
	assert.False(t, IsBrokerRequest(regularReq))
	assert.Equal(t, "/patron_requests/1", WithBrokerPrefix(regularReq, "/patron_requests/1"))
	assert.Equal(t, "/", WithBrokerPrefix(regularReq, ""))
	assert.Equal(t, "/", WithBrokerPrefix(regularReq, "/"))
}

func TestPathAndQuery(t *testing.T) {
	assert.Equal(t, "/patron_requests/1/items", Path("patron_requests", "1", "items"))
	assert.Equal(t, "/patron_requests/1/items", Path("/patron_requests/", "/1/", "/items/"))
	assert.Equal(t, "/patron%20requests/a%2Fb%3Fx/items", Path("patron requests", "a/b?x", "items"))

	values := Query("symbol", "ISIL:REQ", "offset", "10", "dangling")
	assert.Equal(t, "ISIL:REQ", values.Get("symbol"))
	assert.Equal(t, "10", values.Get("offset"))
	assert.Empty(t, values.Get("dangling"))
}

func TestLink(t *testing.T) {
	regularReq := httptest.NewRequest("GET", "https://example.org/patron_requests", nil)
	regularReq.RequestURI = "/patron_requests"
	link := Link(regularReq, Path("patron_requests", "1", "items"), Query("symbol", "ISIL:REQ", "q", "a b"))
	assert.Equal(t, "https://example.org/patron_requests/1/items?q=a+b&symbol=ISIL%3AREQ", link)

	brokerReq := httptest.NewRequest("GET", "https://example.org/broker/patron_requests", nil)
	brokerReq.RequestURI = "/broker/patron_requests"
	brokerLink := Link(brokerReq, Path("patron_requests", "1", "items"), Query("symbol", "ISIL:REQ"))
	assert.Equal(t, "https://example.org/broker/patron_requests/1/items?symbol=ISIL%3AREQ", brokerLink)
}

func TestLinkRel(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.org/patron_requests/1", nil)
	req.RequestURI = "/patron_requests/1"

	currentLink := LinkRel(req, "", Query("symbol", "ISIL:REQ"))
	assert.Equal(t, "https://example.org/patron_requests/1?symbol=ISIL%3AREQ", currentLink)

	relativeLink := LinkRel(req, "items", Query("symbol", "ISIL:REQ"))
	assert.Equal(t, "https://example.org/patron_requests/1/items?symbol=ISIL%3AREQ", relativeLink)
}

func TestCollectAboutDataLastLink(t *testing.T) {
	reqOffset0 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=0", nil)
	reqOffset10 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=10", nil)
	reqOffset20 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=20", nil)
	reqOffset1000 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=1000", nil)

	// First page (count=21, limit=10, offset=0): prevLink omitted, next/last present.
	about := CollectAboutData(21, 0, 10, reqOffset0)
	assert.Equal(t, int64(21), about.Count)
	assert.Nil(t, about.FirstLink)
	assert.Nil(t, about.PrevLink)
	assert.NotNil(t, about.NextLink)
	assert.Contains(t, *about.NextLink, "offset=10")
	assert.Contains(t, *about.NextLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.LastLink)
	assert.Contains(t, *about.LastLink, "offset=20")
	assert.Contains(t, *about.LastLink, "symbol=ISIL%3ADK-BIB1")

	// Not last page (count=21, limit=10, offset=10): all links present
	about = CollectAboutData(21, 10, 10, reqOffset10)
	assert.Equal(t, int64(21), about.Count)
	assert.NotNil(t, about.FirstLink)
	assert.Contains(t, *about.FirstLink, "offset=0")
	assert.Contains(t, *about.FirstLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.PrevLink)
	assert.Contains(t, *about.PrevLink, "offset=0")
	assert.Contains(t, *about.PrevLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.NextLink)
	assert.Contains(t, *about.NextLink, "offset=20")
	assert.Contains(t, *about.NextLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.LastLink)
	assert.Contains(t, *about.LastLink, "offset=20")
	assert.Contains(t, *about.LastLink, "symbol=ISIL%3ADK-BIB1")

	// Last page (count=20, limit=10, offset=10): lastLink and nextLink should be omitted.
	about = CollectAboutData(20, 10, 10, reqOffset10)
	assert.Equal(t, int64(20), about.Count)
	assert.NotNil(t, about.FirstLink)
	assert.Contains(t, *about.FirstLink, "offset=0")
	assert.Contains(t, *about.FirstLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.PrevLink)
	assert.Contains(t, *about.PrevLink, "offset=0")
	assert.Contains(t, *about.PrevLink, "symbol=ISIL%3ADK-BIB1")
	assert.Nil(t, about.NextLink)
	assert.Nil(t, about.LastLink)

	// Last partial page (count=21, limit=10, offset=20): lastLink and nextLink should be omitted.
	about = CollectAboutData(21, 20, 10, reqOffset20)
	assert.Equal(t, int64(21), about.Count)
	assert.NotNil(t, about.FirstLink)
	assert.Contains(t, *about.FirstLink, "offset=0")
	assert.Contains(t, *about.FirstLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.PrevLink)
	assert.Contains(t, *about.PrevLink, "offset=10")
	assert.Contains(t, *about.PrevLink, "symbol=ISIL%3ADK-BIB1")
	assert.Nil(t, about.NextLink)
	assert.Nil(t, about.LastLink)

	// Out-of-range page (count=21, limit=10, offset=1000): prevLink and lastLink should be present.
	about = CollectAboutData(21, 1000, 10, reqOffset1000)
	assert.Equal(t, int64(21), about.Count)
	assert.NotNil(t, about.FirstLink)
	assert.Contains(t, *about.FirstLink, "offset=0")
	assert.Contains(t, *about.FirstLink, "symbol=ISIL%3ADK-BIB1")
	assert.NotNil(t, about.PrevLink)
	assert.Contains(t, *about.PrevLink, "offset=20")
	assert.Contains(t, *about.PrevLink, "symbol=ISIL%3ADK-BIB1")
	assert.Nil(t, about.NextLink)
	assert.NotNil(t, about.LastLink)
	assert.Contains(t, *about.LastLink, "offset=20")
	assert.Contains(t, *about.LastLink, "symbol=ISIL%3ADK-BIB1")
}
