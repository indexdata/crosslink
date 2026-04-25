package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLinkAddsOkapiPrefix(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/broker/", nil)

	link := Link(req, Path("events"), Query("symbol", "ISIL:DK-BIB1"))

	assert.Equal(t, "http://localhost/broker/events?symbol=ISIL%3ADK-BIB1", link)
}

func TestLinkRelPreservesOkapiPrefix(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/broker/ill_transactions?offset=0", nil)

	link := LinkRel(req, "events", Query("offset", "10"))

	assert.Equal(t, "http://localhost/broker/ill_transactions/events?offset=10", link)
}

func TestCollectAboutDataLinksPreserveOkapiPrefix(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/broker/ill_transactions?symbol=ISIL:DK-BIB1&offset=0", nil)

	about := CollectAboutData(21, 0, 10, req)

	assert.NotNil(t, about.NextLink)
	assert.Contains(t, *about.NextLink, "http://localhost/broker/ill_transactions?")
	assert.Contains(t, *about.NextLink, "offset=10")
	assert.NotNil(t, about.LastLink)
	assert.Contains(t, *about.LastLink, "http://localhost/broker/ill_transactions?")
	assert.Contains(t, *about.LastLink, "offset=20")
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

func TestGetProtoUsesForwardedProto(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Proto", "http")
	req := &http.Request{Header: header, URL: &url.URL{Scheme: "https", Host: "broker.example.org"}}

	assert.Equal(t, "http", getProto(req))
}

func TestGetProtoUsesFirstForwardedProtoValue(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Proto", "https, http")
	req := &http.Request{Header: header, URL: &url.URL{Scheme: "http", Host: "broker.example.org"}}

	assert.Equal(t, "https", getProto(req))
}

func TestGetProtoInvalidForwardedProtoFallsBackToHttps(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Proto", "ftp")
	req := &http.Request{Header: header, URL: &url.URL{Scheme: "", Host: "broker.example.org"}}

	assert.Equal(t, "https", getProto(req))
}

func TestGetProtoForcesHttpForLocalhost(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Proto", "https")
	req := &http.Request{Header: header, URL: &url.URL{Scheme: "https", Host: "localhost:9130"}}

	assert.Equal(t, "http", getProto(req))
}

func TestGetProtoDoesNotForceHttpForPartialLocalhostName(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Proto", "https")
	req := &http.Request{Header: header, URL: &url.URL{Scheme: "https", Host: "my-localhost.example:9130"}}

	assert.Equal(t, "https", getProto(req))
}

func TestGetHostUsesFirstForwardedHostValue(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Host", "proxy.example.org, backend.internal")
	req := &http.Request{Header: header, URL: &url.URL{Host: "broker.example.org"}}

	assert.Equal(t, "proxy.example.org", getHost(req))
}
