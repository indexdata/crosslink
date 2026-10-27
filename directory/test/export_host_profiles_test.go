package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectoryExportHostProfiles(t *testing.T) {
	source, err := os.ReadFile("../../migration/export-crosslink-directory.sql")
	require.NoError(t, err)
	_, lmsProjection, found := strings.Cut(string(source), "CROSS JOIN crosslink_tenant_settings AS tenant_settings")
	require.True(t, found)
	lmsProjection, _, found = strings.Cut(lmsProjection, ") AS lms_config ON true")
	require.True(t, found)
	_, catalogProjection, found := strings.Cut(string(source), ") AS branch_lms_config ON true")
	require.True(t, found)
	catalogProjection, _, found = strings.Cut(catalogProjection, ") AS catalog_config ON true")
	require.True(t, found)
	// Execute the export's actual projections against representative mod-rs
	// settings, then import their output through the complete-import contract.
	query := `WITH
	 entry AS (SELECT $2::text AS lms_location_code, 'STACKS'::text AS item_location),
	 local_entry AS (SELECT 1 AS entry_id),
	 tenant_settings AS (SELECT $1::text AS host_lms_integration,
	   $3::text AS ncip_server_address, 'FROM'::text AS ncip_from_agency,
	   'auth'::text AS ncip_from_agency_authentication, 'TO'::text AS ncip_to_agency,
	   'ncip'::text AS borrower_check, 'ncip'::text AS accept_item,
	   'ncip'::text AS check_in_item, 'ncip'::text AS check_out_item,
	   'ncip'::text AS use_request_item, 'yes'::text AS ncip_use_title_request_type,
	   'SUPPLY'::text AS supplier_pickup_location,
	   'PATRON'::text AS default_institutional_patron_id,
	   'catalog.example:210/db'::text AS z3950_server_address),
	 host_lms_patron_profile AS (SELECT 'STAFF'::text AS hlpp_code, 'Staff'::text AS hlpp_name,
	   true AS hlpp_can_create_requests, false AS hlpp_hidden, 1 AS hlpp_id)
	 SELECT lms_config.item, catalog_config.item
	 FROM entry CROSS JOIN local_entry CROSS JOIN tenant_settings
	` + lmsProjection + ") AS lms_config ON true" + catalogProjection + ") AS catalog_config ON true"

	resetImportState(t)
	for _, tc := range []struct {
		name, vendor, version, normalization, requestType, scope, bibCode string
		source                                                            any
		namespace, pickup                                                 bool
	}{
		{"Evergreen", "Evergreen", "2.02", "none", "Loan", "Bibliographic Item", "BibID", "evergreen", true, false},
		{"Horizon", "Horizon", "1.0", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "horizon", true, false},
		{"Polaris", "Polaris", "1.0", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "polaris", true, false},
		{"Symphony", "Symphony", "1.0", "stripLeadingNonDigits", "Loan", "Bibliographic Item", "SYSNUMBER", "symphony", true, false},
		{"Alma", "Alma", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "alma", true, true},
		{"Sierra", "Sierra", "2.02", "sierra", "Hold", "Title", "SYSNUMBER", "sierra", true, true},
		{"Koha", "Koha", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "koha", false, true},
		{"FOLIO", "FOLIO", "2.02", "none", "Page", "Title", "SYSNUMBER", "folio", true, true},
		{"WMS", "WMS", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "wms", true, true},
		{"WMS2", "WMS", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "wms2", true, true},
		{"Aleph", "Aleph", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "aleph", true, true},
		{"unknown", "Generic", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", "unknown", true, true},
		{"missing", "Generic", "2.02", "none", "Loan", "Bibliographic Item", "SYSNUMBER", nil, true, true},
		{"mixed case", "Symphony", "1.0", "stripLeadingNonDigits", "Loan", "Bibliographic Item", "SYSNUMBER", "SyMpHoNy", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"with pickup", "without pickup", "catalog only"} {
				t.Run(mode, func(t *testing.T) {
					var location any = "PICKUP"
					var address any = "https://example.test/ncip"
					if mode == "without pickup" {
						location = nil
					}
					if mode == "catalog only" {
						address = nil
					}
					var rawLms, rawCatalog []byte
					require.NoError(t, dbpool.QueryRow(context.Background(), query, tc.source, location, address).Scan(&rawLms, &rawCatalog))
					var lms, catalog map[string]any
					require.NoError(t, json.Unmarshal(rawCatalog, &catalog))
					require.Equal(t, tc.vendor, catalog["profile"])
					require.Nil(t, catalog["queryConfig"])
					require.Nil(t, catalog["holdingsFormat"])
					if address == nil {
						require.Nil(t, rawLms)
					} else {
						require.NoError(t, json.Unmarshal(rawLms, &lms))
						require.Equal(t, tc.vendor, lms["vendor"])
						require.Equal(t, tc.version, lms["ncipVersion"])
						require.Equal(t, tc.namespace, lms["ncipNamespaceEnabled"])
						require.Equal(t, tc.normalization, lms["bibIdNormalization"])
						require.Equal(t, tc.requestType, lms["requestItemRequestType"])
						require.Equal(t, tc.scope, lms["requestItemRequestScopeType"])
						require.Equal(t, tc.bibCode, lms["requestItemBibIdCode"])
						require.Equal(t, tc.pickup && location != nil, lms["requestItemPickupLocationEnabled"])
						require.Equal(t, location, lms["requesterPickupLocation"])
					}

					key := symbolObject("ISIL", strings.ReplaceAll(tc.name+mode, " ", ""))
					record := entryImportRecord(key, tc.name, nil, "Institution")
					data := record["data"].(map[string]any)
					data["catalogConfig"] = catalog
					if lms != nil {
						data["lmsConfig"] = lms
					}
					for _, policy := range []string{"fail", "update"} {
						response, result := importRequest(t, []any{record}, policy, standardHeaders)
						require.Equal(t, http.StatusOK, response.StatusCode)
						require.Empty(t, result.Errors)
						response, body := jsonReq(t, http.MethodGet, "/entries/by-id/"+importEntryUUID(key).String(), "", standardHeaders)
						require.Equal(t, http.StatusOK, response.StatusCode, body)
						var stored map[string]any
						require.NoError(t, json.Unmarshal([]byte(body), &stored))
						require.Equal(t, tc.vendor, stored["catalogConfig"].(map[string]any)["profile"])
						if lms != nil {
							storedLms := stored["lmsConfig"].(map[string]any)
							for _, field := range []string{"vendor", "ncipVersion", "ncipNamespaceEnabled", "bibIdNormalization", "requestItemPickupLocationEnabled"} {
								require.Equal(t, lms[field], storedLms[field], field)
							}
						}
					}
				})
			}
		})
	}
}
