package test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHostProfilePersistence(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}

	res, data := jsonReq(t, http.MethodPost, "/entries", `{"name":"Profile only","type":"Institution","lmsConfig":{"vendor":"Sierra","requestItemPickupLocationEnabled":false},"catalogConfig":{"profile":"Koha","holdingsFormat":{"marc":{"callNumberSubField":"x"}}}}`, headers)
	require.Equal(t, http.StatusCreated, res.StatusCode, data)
	var created struct {
		Id string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(data), &created))
	path := "/entries/by-id/" + created.Id
	get := func() map[string]any {
		res, data := jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, res.StatusCode, data)
		var e map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &e))
		return e
	}
	e := get()
	l := e["lmsConfig"].(map[string]any)
	require.Equal(t, map[string]any{"vendor": "Sierra", "requestItemPickupLocationEnabled": false}, l)
	c := e["catalogConfig"].(map[string]any)
	require.Equal(t, "Koha", c["profile"])
	require.Equal(t, map[string]any{"marc": map[string]any{"callNumberSubField": "x"}}, c["holdingsFormat"])
	res, data = jsonReq(t, http.MethodPatch, path, `{"lmsConfig":{"ncipNamespaceEnabled":false,"bibIdNormalization":"none"},"catalogConfig":{"profile":null,"holdingsFormat":{"marc":{"locationSubField":"y"}}}}`, headers)
	require.Equal(t, http.StatusNoContent, res.StatusCode, data)
	e = get()
	l = e["lmsConfig"].(map[string]any)
	require.Equal(t, "Sierra", l["vendor"])
	require.Equal(t, false, l["ncipNamespaceEnabled"])
	c = e["catalogConfig"].(map[string]any)
	require.NotContains(t, c, "profile")
	require.Equal(t, map[string]any{"marc": map[string]any{"callNumberSubField": "x", "locationSubField": "y"}}, c["holdingsFormat"])
	res, data = jsonReq(t, http.MethodPatch, path, `{"lmsConfig":{"vendor":null,"ncipNamespaceEnabled":null,"bibIdNormalization":null},"catalogConfig":{"holdingsFormat":{"opac":{"availabilityRule":"publicNote","availablePublicNotes":[]}}}}`, headers)
	require.Equal(t, http.StatusNoContent, res.StatusCode, data)
	e = get()
	l = e["lmsConfig"].(map[string]any)
	require.Equal(t, map[string]any{"requestItemPickupLocationEnabled": false}, l)
	c = e["catalogConfig"].(map[string]any)
	require.Equal(t, map[string]any{"opac": map[string]any{"availabilityRule": "publicNote", "availablePublicNotes": []any{}}}, c["holdingsFormat"])
}
