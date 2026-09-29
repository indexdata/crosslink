package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDirectoryExportPickupLocations(t *testing.T) {
	// Execute the exporter's actual ILL projection against minimal legacy data,
	// then import its output to check the contract and persisted designation.
	source, err := os.ReadFile("../../migration/export-crosslink-directory.sql")
	require.NoError(t, err)
	_, projection, found := strings.Cut(string(source), ") AS catalog_config ON true")
	require.True(t, found)
	projection, _, found = strings.Cut(projection, ") AS ill_config ON true")
	require.True(t, found)
	query := `WITH
		entry AS (SELECT $1::uuid AS entry_id, $2::text AS lms_location_code),
		local_entry AS (SELECT CASE WHEN $3::boolean THEN $1::uuid END AS entry_id),
		tenant_settings AS (SELECT '5'::text AS max_requests_per_patron),
		service AS (SELECT 1 AS se_id, 1 AS se_type_fk, $4::text AS se_address WHERE $4::text IS NOT NULL),
		service_account AS (SELECT 1 AS sa_service, $1::uuid AS sa_account_holder),
		refdata_value AS (SELECT 1 AS rdv_id, 'ISO18626'::text AS rdv_value)
		SELECT ill_config.item FROM entry CROSS JOIN local_entry CROSS JOIN tenant_settings
	` + projection + ") AS ill_config ON true"

	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	institution := symbolObject("ISIL", "INST")
	response, result := importRequest(t, []any{
		entryImportRecord(consortium, "Consortium", nil, "Consortium"),
		entryImportRecord(institution, "Institution", consortium, "Institution"),
	}, "", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Empty(t, result.Errors)
	code, empty, endpoint := "main", "", "https://example.test/iso18626"
	for i, tc := range []struct {
		name     string
		code     *string
		local    bool
		endpoint *string
	}{
		{"local with endpoint", &code, true, &endpoint},
		{"local without endpoint", &code, true, nil},
		{"branch without endpoint", &code, false, nil},
		{"branch with endpoint", &code, false, &endpoint},
		{"empty code", &empty, false, nil},
		{"local without code", nil, true, &endpoint},
		{"branch without code", nil, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var exported []byte
			require.NoError(t, dbpool.QueryRow(context.Background(), query, uuid.New(), tc.code, tc.local, tc.endpoint).Scan(&exported))
			var config map[string]any
			if exported != nil {
				require.NoError(t, json.Unmarshal(exported, &config))
				require.Equal(t, tc.code != nil, config["isPickupLocation"])
				if tc.local {
					require.Equal(t, float64(5), config["maxRequestsPerPatron"])
					if tc.endpoint != nil {
						require.Equal(t, *tc.endpoint, config["iso18626Url"])
					} else {
						require.Nil(t, config["iso18626Url"])
					}
				} else {
					require.Nil(t, config["maxRequestsPerPatron"])
					require.Nil(t, config["iso18626Url"])
				}
			} else {
				require.False(t, tc.local)
				require.Nil(t, tc.code)
			}
			key := symbolObject("ISIL", fmt.Sprintf("PICKUP%d", i))
			entryType := "Institution"
			parent := consortium
			if !tc.local {
				entryType = "Branch"
				parent = institution
			}
			record := entryImportRecord(key, tc.name, parent, entryType)
			record["data"].(map[string]any)["illConfig"] = config
			for _, policy := range []string{"fail", "update"} {
				response, result := importRequest(t, []any{record}, policy, standardHeaders)
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Empty(t, result.Errors)
				id := importedEntryID(t, "ISIL", key["symbol"].(string))
				var enabled bool
				require.NoError(t, dbpool.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM ill_configs WHERE entry=$1 AND is_pickup_location)", id).Scan(&enabled))
				require.Equal(t, tc.code != nil, enabled)
			}
		})
	}
}
