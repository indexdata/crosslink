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

func TestCatalogEndpointConflicts(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	const both = `{"sru":{"address":"https://catalog.example/sru"},"zoom":{"address":"catalog.example:210/db"}}`
	res, data := jsonReq(t, http.MethodPost, "/entries", `{"name":"Conflicting endpoints","type":"Institution","catalogConfig":`+both+`}`, headers)
	require.Equal(t, http.StatusBadRequest, res.StatusCode, data)
	require.Contains(t, data, "both SRU and ZOOM")

	for _, tc := range []struct {
		name, initial, update, conflict string
	}{
		{"SRU", `{"sru":{"address":"https://catalog.example/sru"}}`, `{"sru":{"recordSchema":"mods"}}`, `{"zoom":{"address":"catalog.example:210/db"}}`},
		{"ZOOM", `{"zoom":{"address":"catalog.example:210/db"}}`, `{"zoom":{"options":{"count":"20"}}}`, `{"sru":{"address":"https://catalog.example/sru"}}`},
		{"no endpoint", `{}`, `{"profile":"Generic"}`, both},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, data := jsonReq(t, http.MethodPost, "/entries", `{"name":"Endpoint validation","type":"Institution","catalogConfig":`+tc.initial+`}`, headers)
			require.Equal(t, http.StatusCreated, res.StatusCode, data)
			var created struct{ Id string }
			require.NoError(t, json.Unmarshal([]byte(data), &created))
			path := "/entries/by-id/" + created.Id
			res, data = jsonReq(t, http.MethodPatch, path, `{"catalogConfig":`+tc.update+`}`, headers)
			require.Equal(t, http.StatusNoContent, res.StatusCode, data)
			res, before := jsonReq(t, http.MethodGet, path, "", headers)
			require.Equal(t, http.StatusOK, res.StatusCode, before)
			res, data = jsonReq(t, http.MethodPatch, path, `{"name":"Rejected change","catalogConfig":`+tc.conflict+`}`, headers)
			require.Equal(t, http.StatusBadRequest, res.StatusCode, data)
			require.Contains(t, data, "both SRU and ZOOM")
			res, after := jsonReq(t, http.MethodGet, path, "", headers)
			require.Equal(t, http.StatusOK, res.StatusCode, after)
			require.JSONEq(t, before, after)
		})
	}
}

func TestHoldingsParserConflicts(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	res, data := jsonReq(t, http.MethodPost, "/entries", `{"name":"Parser validation","type":"Institution","catalogConfig":{"holdingsFormat":{"marc":{"mainField":"999"}}}}`, headers)
	require.Equal(t, http.StatusCreated, res.StatusCode, data)
	var created struct{ Id string }
	require.NoError(t, json.Unmarshal([]byte(data), &created))
	path := "/entries/by-id/" + created.Id
	res, before := jsonReq(t, http.MethodGet, path, "", headers)
	require.Equal(t, http.StatusOK, res.StatusCode, before)

	parsers := []string{"marc", "opac", "reservoir", "marc21plus1"}
	for i, first := range parsers {
		for _, second := range parsers[i+1:] {
			t.Run(first+"/"+second, func(t *testing.T) {
				body := `{"name":"Rejected change","type":"Institution","catalogConfig":{"holdingsFormat":{"` + first + `":{},"` + second + `":{}}}}`
				for _, method := range []string{http.MethodPost, http.MethodPatch} {
					endpoint := "/entries"
					if method == http.MethodPatch {
						endpoint = path
					}
					res, data := jsonReq(t, method, endpoint, body, headers)
					require.Equal(t, http.StatusBadRequest, res.StatusCode, data)
					require.Contains(t, data, "at most one of marc, opac, reservoir, or marc21plus1")
				}
			})
		}
	}
	res, after := jsonReq(t, http.MethodGet, path, "", headers)
	require.Equal(t, http.StatusOK, res.StatusCode, after)
	require.JSONEq(t, before, after)

	// Empty patches preserve overrides; selecting a different parser replaces them.
	for _, parser := range append([]string{""}, parsers...) {
		holdings := `{}`
		if parser != "" {
			holdings = `{"` + parser + `":{}}`
		}
		res, data = jsonReq(t, http.MethodPost, "/entries", `{"name":"Valid parser","type":"Institution","catalogConfig":{"holdingsFormat":`+holdings+`}}`, headers)
		require.Equal(t, http.StatusCreated, res.StatusCode, data)
		res, data = jsonReq(t, http.MethodPatch, path, `{"catalogConfig":{"holdingsFormat":`+holdings+`}}`, headers)
		require.Equal(t, http.StatusNoContent, res.StatusCode, data)
		res, data = jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, res.StatusCode, data)
		var saved struct {
			CatalogConfig struct{ HoldingsFormat map[string]any }
		}
		require.NoError(t, json.Unmarshal([]byte(data), &saved))
		require.Len(t, saved.CatalogConfig.HoldingsFormat, 1)
		if parser == "" || parser == "marc" {
			require.Equal(t, map[string]any{"mainField": "999"}, saved.CatalogConfig.HoldingsFormat["marc"])
		} else {
			require.Contains(t, saved.CatalogConfig.HoldingsFormat, parser)
		}
	}
}
