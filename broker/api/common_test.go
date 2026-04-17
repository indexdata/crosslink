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

func TestCollectAboutDataLastLink(t *testing.T) {
	reqOffset0 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=0", nil)
	reqOffset10 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=10", nil)
	reqOffset20 := httptest.NewRequest("GET", "http://localhost/ill_transactions?symbol=ISIL:DK-BIB1&offset=20", nil)

	// First page (count=21, limit=10, offset=0): prevLink omitted, next/last present.
	about := CollectAboutData(21, 0, 10, reqOffset0)
	assert.Equal(t, int64(21), about.Count)
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
	assert.NotNil(t, about.PrevLink)
	assert.Contains(t, *about.PrevLink, "offset=0")
	assert.Contains(t, *about.PrevLink, "symbol=ISIL%3ADK-BIB1")
	assert.Nil(t, about.NextLink)
	assert.Nil(t, about.LastLink)

	// Last partial page (count=21, limit=10, offset=20): lastLink and nextLink should be omitted.
	about = CollectAboutData(21, 20, 10, reqOffset20)
	assert.Equal(t, int64(21), about.Count)
	assert.NotNil(t, about.PrevLink)
	assert.Contains(t, *about.PrevLink, "offset=10")
	assert.Contains(t, *about.PrevLink, "symbol=ISIL%3ADK-BIB1")
	assert.Nil(t, about.NextLink)
	assert.Nil(t, about.LastLink)
}
