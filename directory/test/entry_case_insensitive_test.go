package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEntryCQLCaseInsensitive(t *testing.T) {
	resetDb()
	ownedID, otherID, decoyID := uuid.New(), uuid.New(), uuid.New()
	_, err := dbpool.Exec(context.Background(), `
		INSERT INTO entries (id, name, description, type, tenant) VALUES
		($1, 'Central Library', 'Regional Research Centre', 'Institution', 'case-search'),
		($2, 'Central Library', 'Regional Research Centre', 'Institution', 'case-other'),
		($3, 'Branch Office', NULL, 'Institution', 'case-search')
	`, ownedID, otherID, decoyID)
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		query string
		owned bool
		ids   []uuid.UUID
	}{
		{"name equality", `name="cENTRAL lIBRARY"`, false, []uuid.UUID{ownedID, otherID}},
		{"name double equals", `name=="CENTRAL LIBRARY"`, false, []uuid.UUID{ownedID, otherID}},
		{"name exact", `name exact "central library"`, false, []uuid.UUID{ownedID, otherID}},
		{"name substring", `name="*lIbRaRy*"`, false, []uuid.UUID{ownedID, otherID}},
		{"name inequality", `name<>"CENTRAL LIBRARY"`, false, []uuid.UUID{decoyID}},
		{"name wildcard inequality", `name<>"*LIBRARY*"`, false, []uuid.UUID{decoyID}},
		{"description equality", `description="rEGIONAL rESEARCH cENTRE"`, false, []uuid.UUID{ownedID, otherID}},
		{"description double equals", `description=="REGIONAL RESEARCH CENTRE"`, false, []uuid.UUID{ownedID, otherID}},
		{"description exact", `description exact "regional research centre"`, false, []uuid.UUID{ownedID, otherID}},
		{"description substring", `description="*rEsEaRcH*"`, false, []uuid.UUID{ownedID, otherID}},
		{"description inequality excludes NULL", `description<>"UNRELATED"`, false, []uuid.UUID{ownedID, otherID}},
		{"combined fields", `name="*LIBRARY*" AND description="*research*"`, false, []uuid.UUID{ownedID, otherID}},
		{"owned name", `name="*LIBRARY*"`, true, []uuid.UUID{ownedID}},
		{"owned description", `description="*RESEARCH*"`, true, []uuid.UUID{ownedID}},
		{"owned OR stays scoped", `name="*LIBRARY*" OR description="*RESEARCH*"`, true, []uuid.UUID{ownedID}},
		{"type remains case sensitive", `type="institution"`, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := "/entries"
			query := `(tenant="case-search" OR tenant="case-other") AND (` + tc.query + `)`
			headers := map[string]string{"X-Okapi-Permissions": `["directory.system.all"]`}
			if tc.owned {
				endpoint = "/entries/owned"
				query = tc.query
				headers = map[string]string{
					"X-Okapi-Tenant":      "case-search",
					"X-Okapi-Permissions": `["directory.institution.all"]`,
				}
			}
			response, body := jsonReq(t, http.MethodGet, endpoint+"?cql="+url.QueryEscape(query), "", headers)
			require.Equal(t, http.StatusOK, response.StatusCode, body)
			var result struct {
				Items []struct{ ID uuid.UUID } `json:"items"`
				About struct{ Count int }      `json:"about"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &result))
			require.Equal(t, len(tc.ids), result.About.Count)
			var actual []uuid.UUID
			for _, item := range result.Items {
				actual = append(actual, item.ID)
			}
			require.ElementsMatch(t, tc.ids, actual)
		})
	}
}
