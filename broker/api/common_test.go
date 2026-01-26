package api

import (
	"net/http"
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
