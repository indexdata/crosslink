package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDirectoryExportBranchLmsConfig(t *testing.T) {
	source, err := os.ReadFile("../../migration/export-crosslink-directory.sql")
	require.NoError(t, err)
	_, projection, found := strings.Cut(string(source), ") AS lms_config ON true")
	require.True(t, found)
	projection, _, found = strings.Cut(projection, ") AS branch_lms_config ON true")
	require.True(t, found)
	query := `WITH
		entry AS (SELECT $1::uuid AS entry_id, $2::uuid AS parent_id, $3::text AS lms_location_code),
		crosslink_local_entry AS (SELECT $4::uuid AS entry_id),
		tenant_settings AS (SELECT $5::text AS ncip_server_address, 'FROM'::text AS ncip_from_agency)
		SELECT branch_lms_config.item FROM entry CROSS JOIN tenant_settings
	` + projection + ") AS branch_lms_config ON true"

	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	institution := symbolObject("ISIL", "INST")
	response, result := importRequest(t, []any{
		entryImportRecord(consortium, "Consortium", nil, "Consortium"),
		entryImportRecord(institution, "Institution", consortium, "Institution"),
	}, "", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Empty(t, result.Errors)
	owner := importEntryUUID(institution)
	code, blank, ncipAddress := "NO_CENTRAL", "  ", "https://example.test/ncip"
	for _, tc := range []struct {
		name        string
		parent      uuid.UUID
		code        *string
		ncipAddress *string
		exported    bool
	}{
		{"direct child with code", owner, &code, &ncipAddress, true},
		{"direct child with blank code", owner, &blank, &ncipAddress, false},
		{"direct child without code", owner, nil, &ncipAddress, false},
		{"not a direct child", uuid.New(), &code, &ncipAddress, false},
		{"tenant without NCIP", owner, &code, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var exported []byte
			require.NoError(t, dbpool.QueryRow(context.Background(), query, uuid.New(), tc.parent, tc.code, owner, tc.ncipAddress).Scan(&exported))
			if !tc.exported {
				require.Nil(t, exported)
				return
			}
			var config map[string]any
			require.NoError(t, json.Unmarshal(exported, &config))
			require.Len(t, config, 22)
			require.Equal(t, code, config["requesterPickupLocation"])
			require.Equal(t, true, config["requestItemPickupLocationEnabled"])
			require.Equal(t, ncipAddress, config["address"])
			require.Equal(t, "FROM", config["fromAgency"])
			require.Nil(t, config["fromAgencyAuthentication"])
			require.Nil(t, config["vendor"])

			key := symbolObject("ISIL", "BRANCH")
			record := entryImportRecord(key, "Branch", institution, "Branch")
			data := record["data"].(map[string]any)
			data["lmsLocationCode"] = code
			data["lmsConfig"] = config
			for _, policy := range []string{"fail", "update"} {
				response, result := importRequest(t, []any{record}, policy, standardHeaders)
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Empty(t, result.Errors)
				var pickupLocation string
				var authentication *string
				require.NoError(t, dbpool.QueryRow(
					context.Background(),
					"SELECT requester_pickup_location, from_agency_authentication FROM lms_configs WHERE entry=$1",
					importedEntryID(t, "ISIL", "BRANCH"),
				).Scan(&pickupLocation, &authentication))
				require.Equal(t, code, pickupLocation)
				require.Nil(t, authentication)
			}
		})
	}
}
