package dirmock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	directory "github.com/indexdata/crosslink/illmock/dirmock/api"
	"github.com/stretchr/testify/require"
)

func TestGetOwnedEntries(t *testing.T) {
	mock, err := NewJson(`[
		{"id":"00000000-0000-0000-0000-000000000003","name":"Zulu","type":"Institution","tenant":"tenant-a"},
		{"id":"00000000-0000-0000-0000-000000000004","name":"Other tenant","type":"Institution","tenant":"tenant-b"},
		{"id":"00000000-0000-0000-0000-000000000001","name":"Alpha","type":"Institution","tenant":"tenant-a"},
		{"id":"00000000-0000-0000-0000-000000000005","name":"Unowned","type":"Institution"},
		{"id":"00000000-0000-0000-0000-000000000002","name":"Branch","type":"Branch","tenant":"tenant-a","parent":"00000000-0000-0000-0000-000000000001"}
	]`)
	require.NoError(t, err)
	mux := http.NewServeMux()
	require.NoError(t, mock.HandlerFromMux(mux))

	for _, tc := range []struct {
		name   string
		tenant string
		query  string
		status int
		names  []string
		total  int64
	}{
		{name: "tenant scope and ordering", tenant: "tenant-a", status: http.StatusOK, names: []string{"Alpha", "Branch", "Zulu"}, total: 3},
		{name: "other tenant", tenant: "tenant-b", status: http.StatusOK, names: []string{"Other tenant"}, total: 1},
		{name: "unknown tenant", tenant: "unknown", status: http.StatusOK},
		{name: "missing tenant", status: http.StatusBadRequest},
		{name: "pagination within tenant", tenant: "tenant-a", query: "limit=1&offset=1", status: http.StatusOK, names: []string{"Branch"}, total: 3},
		{name: "CQL or cannot bypass scope", tenant: "tenant-a", query: "cql=" + url.QueryEscape(`tenant = tenant-b or name = Alpha`), status: http.StatusOK, names: []string{"Alpha"}, total: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/directory/entries/owned?"+tc.query, nil)
			request.Header.Set("X-Okapi-Tenant", tc.tenant)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.status != http.StatusOK {
				return
			}

			var response directory.EntriesResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.NotNil(t, response.Items, "empty results must be an array")
			require.Equal(t, tc.total, response.About.Count)
			var names []string
			for _, entry := range response.Items {
				require.NotNil(t, entry.Tenant)
				require.Equal(t, tc.tenant, *entry.Tenant)
				names = append(names, entry.Name)
			}
			require.Equal(t, tc.names, names)
		})
	}
}
