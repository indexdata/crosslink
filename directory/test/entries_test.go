package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestEntryCases(t *testing.T) {

	consortiumPermissionHeaders := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	institutionPermissionHeaders := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	cases := []httpTestCase{
		{
			name:        "GET without symbols",
			method:      http.MethodGet,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "entry-nosym.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		/*
			{
				name:        "GET owned entry",
				method:      http.MethodGet,
				endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000001",
				status:      http.StatusOK,
				resFile:     "entry-diku.get.res.json",
				addlHeaders: dikuPermissionHeaders,
			},
		*/
		{
			name:        "GET",
			method:      http.MethodGet,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000002",
			status:      http.StatusOK,
			resFile:     "entry.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:     "GET no permissions",
			method:   http.MethodGet,
			endpoint: "/entries/by-id/00000000-0000-0000-0000-000000000002",
			status:   http.StatusUnauthorized,
		},
		{
			name:        "GET by symbol",
			method:      http.MethodGet,
			endpoint:    "/entries/by-symbol/TEST:ANINST",
			status:      http.StatusOK,
			resFile:     "entry.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET consortium by symbol",
			method:      http.MethodGet,
			endpoint:    "/entries/by-symbol/TEST:ANCONS",
			status:      http.StatusOK,
			resFile:     "entry-consortium.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:     "GET by symbol no permissions",
			method:   http.MethodGet,
			endpoint: "/entries/by-symbol/TEST:ANINST",
			status:   http.StatusUnauthorized,
		},
		{
			name:        "GET by invalid symbol",
			method:      http.MethodGet,
			endpoint:    "/entries/by-symbol/TESTANINST",
			status:      http.StatusBadRequest,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET by invalid id",
			method:      http.MethodGet,
			endpoint:    "/entries/by-id/not-an-id",
			status:      http.StatusBadRequest,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET id not found",
			method:      http.MethodGet,
			endpoint:    "/entries/by-id/f0000000-0000-0000-0000-000000000002",
			status:      http.StatusNotFound,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET symbol not found",
			method:      http.MethodGet,
			endpoint:    "/entries/by-symbol/TEST:NOPE",
			status:      http.StatusNotFound,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries",
			method:      http.MethodGet,
			endpoint:    "/entries",
			status:      http.StatusOK,
			resFile:     "entries.get.res.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:     "GET entries no perms",
			method:   http.MethodGet,
			endpoint: "/entries",
			status:   http.StatusUnauthorized,
		},
		{
			name:        "GET entries with CQL query by name",
			method:      http.MethodGet,
			endpoint:    "/entries?cql=name%3DAn%20Institution",
			status:      http.StatusOK,
			resFile:     "entries-cql-name.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with CQL query by parent",
			method:      http.MethodGet,
			endpoint:    "/entries?cql=parent%3D00000000-0000-0000-0000-000000000004",
			status:      http.StatusOK,
			resFile:     "entries-cql-parent.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with CQL wildcard query by description",
			method:      http.MethodGet,
			endpoint:    "/entries?cql=description%3D%2Aparticular%2A",
			status:      http.StatusOK,
			resFile:     "entries-cql-desc.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with invalid CQL query",
			method:      http.MethodGet,
			endpoint:    "/entries?cql=invalid%28%28%28",
			status:      http.StatusBadRequest,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with CQL query by type",
			method:      http.MethodGet,
			endpoint:    "/entries?cql=type%3DConsortium",
			status:      http.StatusOK,
			resFile:     "entries.get.res.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:            "POST entry",
			method:          http.MethodPost,
			endpoint:        "/entries",
			status:          http.StatusCreated,
			bodyFile:        "entry.post.req.json",
			refetchEndpoint: "/entries/by-id",
			refetchFile:     "entry.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:        "POST entry, insufficient perms",
			method:      http.MethodPost,
			endpoint:    "/entries",
			status:      http.StatusUnauthorized,
			bodyFile:    "entry.post.req.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "POST entry dupe symbol",
			method:      http.MethodPost,
			endpoint:    "/entries",
			status:      http.StatusBadRequest,
			bodyFile:    "entry-dupe-sym.post.req.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000002",
			status:      http.StatusNoContent,
			bodyFile:    "entry.patch.req.json",
			refetchFile: "entry.patch.refetch.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "PATCH entry by symbol",
			method:          http.MethodPatch,
			endpoint:        "/entries/by-symbol/TEST:ANINST",
			status:          http.StatusNoContent,
			bodyFile:        "entry.patch2.req.json",
			refetchEndpoint: "/entries/by-id/00000000-0000-0000-0000-000000000002",
			refetchFile:     "entry.patch2.refetch.json",
			addlHeaders:     institutionPermissionHeaders,
		},
		{
			name:        "PATCH id not found",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/f0000000-0000-0000-0000-000000000002",
			bodyFile:    "entry.patch.req.json",
			status:      http.StatusNotFound,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "PATCH entry dupe symbol",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000001",
			status:      http.StatusBadRequest,
			bodyFile:    "entry-dupe-sym.patch.req.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "DELETE entry by symbol insuffient permissions",
			method:      http.MethodDelete,
			endpoint:    "/entries/by-symbol/TEST:ANINST",
			status:      http.StatusUnauthorized,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:          "DELETE entry",
			method:        http.MethodDelete,
			endpoint:      "/entries/by-id/00000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
		{
			name:          "DELETE entry by symbol",
			method:        http.MethodDelete,
			endpoint:      "/entries/by-symbol/TEST:ANINST",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
		{
			name:            "POST entry with addresses",
			method:          http.MethodPost,
			endpoint:        "/entries",
			status:          http.StatusCreated,
			bodyFile:        "entry-with-address.post.req.json",
			refetchEndpoint: "/entries/by-id",
			refetchFile:     "entry-with-address.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry addresses",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			status:      http.StatusNoContent,
			bodyFile:    "entry-with-address.patch.req.json",
			refetchFile: "entry-with-address.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry addresses to null",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			status:      http.StatusNoContent,
			body:        `{"addresses": null}`,
			addlHeaders: consortiumPermissionHeaders,
			resFunc: func(res *http.Response, data string) bool {
				if res.StatusCode != http.StatusNoContent {
					return false
				}
				// Verify no addresses remain in database
				var count int
				err := dbpool.QueryRow(context.Background(),
					"SELECT COUNT(*) FROM addresses WHERE entry = '00000000-0000-0000-0000-000000000003'").Scan(&count)
				if err != nil || count != 0 {
					return false
				}
				// Verify no address components remain either
				err = dbpool.QueryRow(context.Background(),
					"SELECT COUNT(*) FROM address_components WHERE address IN (SELECT id FROM addresses WHERE entry = '00000000-0000-0000-0000-000000000003')").Scan(&count)
				return err == nil && count == 0
			},
		},
		{
			name:        "PATCH entry symbols to null",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000002",
			status:      http.StatusNoContent,
			body:        `{"symbols": null}`,
			addlHeaders: institutionPermissionHeaders,
			resFunc: func(res *http.Response, data string) bool {
				var count int
				err := dbpool.QueryRow(context.Background(),
					"SELECT COUNT(*) FROM symbols WHERE owner = '00000000-0000-0000-0000-000000000002'").Scan(&count)
				return err == nil && count == 0
			},
		},
		{
			name:        "PATCH entry endpoints to null",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000002",
			status:      http.StatusNoContent,
			body:        `{"endpoints": null}`,
			addlHeaders: institutionPermissionHeaders,
			resFunc: func(res *http.Response, data string) bool {
				var count int
				err := dbpool.QueryRow(context.Background(),
					"SELECT COUNT(*) FROM service_endpoints WHERE entry = '00000000-0000-0000-0000-000000000002'").Scan(&count)
				return err == nil && count == 0
			},
		},
		{
			name:        "PATCH entry type to null",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000002",
			body:        `{"type":null}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry with duplicate symbols in request",
			method:      http.MethodPost,
			endpoint:    "/entries",
			status:      http.StatusBadRequest,
			body:        `{"name":"Test","symbols":[{"authority":"DUP","symbol":"SYM"},{"authority":"DUP","symbol":"SYM"}]}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry with empty name",
			method:      http.MethodPost,
			endpoint:    "/entries",
			status:      http.StatusBadRequest,
			body:        `{"name":""}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry adding duplicate symbol from another entry",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000001",
			status:      http.StatusBadRequest,
			body:        `{"symbols":[{"authority":"TEST","symbol":"ANINST"}]}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "GET by symbol without colon",
			method:      http.MethodGet,
			endpoint:    "/entries/by-symbol/NOSYMBOLHERE",
			status:      http.StatusBadRequest,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "PATCH by symbol without colon",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-symbol/NOSYMBOLHERE",
			status:      http.StatusBadRequest,
			body:        `{"name":"Updated"}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "DELETE by symbol without colon",
			method:      http.MethodDelete,
			endpoint:    "/entries/by-symbol/NOSYMBOLHERE",
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "GET entries with limit",
			method:      http.MethodGet,
			endpoint:    "/entries?limit=2",
			status:      http.StatusOK,
			resFile:     "entries-limit-2.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with offset",
			method:      http.MethodGet,
			endpoint:    "/entries?offset=2",
			status:      http.StatusOK,
			resFile:     "entries-offset-2.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with limit and offset",
			method:      http.MethodGet,
			endpoint:    "/entries?limit=1&offset=1",
			status:      http.StatusOK,
			resFile:     "entries-limit-1-offset-1.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with negative limit rejected",
			method:      http.MethodGet,
			endpoint:    "/entries?limit=-1",
			status:      http.StatusBadRequest,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST entry with embedded lmsConfig",
			method:          http.MethodPost,
			endpoint:        "/entries",
			bodyFile:        "entry-with-lmsconfig.post.req.json",
			status:          http.StatusCreated,
			refetchEndpoint: "/entries/by-id",
			refetchFile:     "entry-with-lmsconfig.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry with embedded lmsConfig",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000001",
			bodyFile:    "entry-with-lmsconfig.patch.req.json",
			status:      http.StatusNoContent,
			refetchFile: "entry-with-lmsconfig.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:     "PATCH entry with embedded lmsConfig, omit required fields",
			method:   http.MethodPatch,
			endpoint: "/entries/by-id/00000000-0000-0000-0000-000000000001",
			bodyFile: "entry-with-lmsconfig-2.patch.req.json",
			status:   http.StatusNoContent,
			//refetchFile: "entry-with-lmsconfig.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry with embedded lmsConfig, check other lms fields",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000002",
			bodyFile:    "entry-with-lmsconfig-3.patch.req.json",
			status:      http.StatusNoContent,
			refetchFile: "entry-with-lmsconfig-3.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry with new lmsconfig",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			bodyFile:    "entry-new-lmsconfig.patch.req.json",
			status:      http.StatusNoContent,
			refetchFile: "entry-new-lmsconfig.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry with new incomplete lmsconfig",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			bodyFile:    "entry-new-lmsconfig-incomplete.patch.req.json",
			status:      http.StatusNoContent,
			refetchFile: "entry-new-lmsconfig-incomplete.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry of type Institution with Institution as parent",
			method:      http.MethodPost,
			endpoint:    "/entries",
			bodyFile:    "entry-new-institution-bad-parent.post.req.json",
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry with parent and default Institution type",
			method:      http.MethodPost,
			endpoint:    "/entries",
			body:        `{"name":"Default Type Institution","parent":"00000000-0000-0000-0000-000000000004"}`,
			status:      http.StatusCreated,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry of type Institution with non-existing id as parent",
			method:      http.MethodPost,
			endpoint:    "/entries",
			bodyFile:    "entry-new-institution-nonexistent-parent.post.req.json",
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry of type Consortium",
			method:      http.MethodPost,
			endpoint:    "/entries",
			bodyFile:    "entry-new-consortium.post.req.json",
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry to type Consortium",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			body:        `{"type":"Consortium"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry with non-existent parent",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			body:        `{"parent":"00000000-0000-0000-3000-000000000001"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry with invalid parent",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000003",
			body:        `{"parent":"00000000-0000-0000-0000-000000000001"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry to be its own parent",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000004",
			body:        `{"type":"Institution","parent":"00000000-0000-0000-0000-000000000004"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH entry type when existing children would become invalid",
			method:      http.MethodPatch,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000004",
			body:        `{"type":"Institution"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry of type Branch with Institution as parent",
			method:      http.MethodPost,
			endpoint:    "/entries",
			bodyFile:    "entry-new-branch.post.req.json",
			status:      http.StatusCreated,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST entry of type Branch with Consortium as parent",
			method:      http.MethodPost,
			endpoint:    "/entries",
			bodyFile:    "entry-new-branch-consortium-parent.post.req.json",
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
	}
	testCases(t, cases)
}

func TestPatchEntryLenderOfLastResortToNull(t *testing.T) {
	resetDb()

	headers := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	_, err := dbpool.Exec(
		context.Background(),
		"INSERT INTO ill_configs (entry, lenders_of_last_resort) VALUES ($2, ARRAY[$1]::text[])",
		"TEST:PATCHED-LOR",
		"00000000-0000-0000-0000-000000000002",
	)
	if err != nil {
		t.Fatalf("failed to seed lender_of_last_resort: %v", err)
	}

	res, data := jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002", `{"illConfig":null}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected PATCH response status of %d, got %d and body of %s", http.StatusNoContent, res.StatusCode, data)
	}

	var illConfigCount int
	err = dbpool.QueryRow(
		context.Background(),
		"SELECT count(*) FROM ill_configs WHERE entry = $1",
		"00000000-0000-0000-0000-000000000002",
	).Scan(&illConfigCount)
	if err != nil {
		t.Fatalf("failed to fetch ill config count: %v", err)
	}
	if illConfigCount != 0 {
		t.Fatalf("expected illConfig row to be deleted, got count %d", illConfigCount)
	}
}

func TestPatchEntryLMSConfigNullValues(t *testing.T) {
	resetDb()

	const entryID = "00000000-0000-0000-0000-000000000002"
	headers := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	var originalAddress, originalFromAgency string
	var originalRequesterPatronPattern *string
	err := dbpool.QueryRow(
		context.Background(),
		"SELECT address, from_agency, requester_patron_pattern FROM lms_configs WHERE entry = $1",
		entryID,
	).Scan(&originalAddress, &originalFromAgency, &originalRequesterPatronPattern)
	if err != nil {
		t.Fatalf("failed to fetch original LMS config: %v", err)
	}

	res, data := jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, `{
		"lmsConfig":{
			"toAgency":null,
			"acceptItemEnabled":null,
			"itemLocation":""
		}
	}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected nested LMS null PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}

	var address, fromAgency string
	var toAgency, itemLocation, requesterPatronPattern *string
	var acceptItemEnabled *bool
	err = dbpool.QueryRow(
		context.Background(),
		`SELECT address, from_agency, to_agency, accept_item_enabled, item_location, requester_patron_pattern
		 FROM lms_configs WHERE entry = $1`,
		entryID,
	).Scan(&address, &fromAgency, &toAgency, &acceptItemEnabled, &itemLocation, &requesterPatronPattern)
	if err != nil {
		t.Fatalf("failed to fetch patched LMS config: %v", err)
	}
	if toAgency != nil || acceptItemEnabled != nil {
		t.Fatalf("expected explicitly null LMS fields to be cleared, got toAgency=%v acceptItemEnabled=%v", toAgency, acceptItemEnabled)
	}
	if itemLocation == nil || *itemLocation != "" {
		t.Fatalf("expected explicit empty itemLocation to remain an empty string, got %v", itemLocation)
	}
	if address != originalAddress || fromAgency != originalFromAgency ||
		!reflect.DeepEqual(requesterPatronPattern, originalRequesterPatronPattern) {
		t.Fatalf("nested LMS null PATCH changed omitted fields")
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, `{"lmsConfig":{"address":null,"fromAgency":null}}`, headers)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected required LMS null PATCH status %d, got %d and body %s", http.StatusBadRequest, res.StatusCode, data)
	}
	err = dbpool.QueryRow(
		context.Background(),
		"SELECT address, from_agency FROM lms_configs WHERE entry = $1",
		entryID,
	).Scan(&address, &fromAgency)
	if err != nil {
		t.Fatalf("failed to refetch LMS config after rejected PATCH: %v", err)
	}
	if address != originalAddress || fromAgency != originalFromAgency {
		t.Fatalf("rejected required-field null PATCH changed LMS config")
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, `{"lmsConfig":null}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected top-level LMS null PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	var count int
	err = dbpool.QueryRow(context.Background(), "SELECT count(*) FROM lms_configs WHERE entry = $1", entryID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count LMS configs after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected top-level LMS null PATCH to delete the config, got count %d", count)
	}

	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+entryID, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after LMS config delete status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	if strings.Contains(data, `"lmsConfig"`) {
		t.Fatalf("lmsConfig should be omitted after top-level null PATCH: %s", data)
	}
}

func TestEntryDirectoryContractFieldsAndCatalogConfig(t *testing.T) {
	resetDb()

	headers := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	body := `{
		"name":"Contract Test Entry",
		"type":"Institution",
		"parent":"00000000-0000-0000-0000-000000000004",
		"email":"contact@example.org",
		"fromEmail":"from@example.org",
		"description":"Contract description",
		"hrid":"contract-hrid",
		"tenant":"contract-tenant",
		"vendor":"CrossLink",
		"illConfig":{
			"iso18626Url":"https://iso.example.org/iso18626",
			"iso18626Vendor":"ReShare",
			"lendersOfLastResort":[{"authority":"ISIL","symbol":"CONTRACT-LOR"}],
			"includeRequestingAgencyInfo":true,
			"includeSupplierInfo":false,
			"includeReturnInfo":true,
			"includeVendorNote":false,
			"useOfferedCosts":true,
			"noteFieldSeparator":" | ",
			"supplierPatronPattern":"PATRON-{requesterSymbol}",
			"duplicateCheckWindowHours":24
		},
		"symbols":[{"authority":"ISIL","symbol":"CONTRACT"}],
		"catalogConfig":{
			"metadataUpdateMode":"merge",
			"zoom":{
				"address":"z3950.example.org:210/catalog",
				"options":{
					"preferredRecordSyntax":"usmarc",
					"count":"20",
					"location":"STACKS",
					"customOption":"preserved"
				}
			},
			"queryConfig":{
				"type":"cql",
				"identifier":"rec.id = {term}",
				"isbn":"isbn = {term}",
				"issn":"issn = {term}",
				"title":"title = {term}"
			},
			"holdingsFormat":{
				"marc":{
					"mainField":"999",
					"itemIdSubField":"i",
					"locationSubField":"l",
					"callNumberSubField":"c",
					"restrictedSubField":"r",
					"shelvingLocationSubField":"s"
				},
				"opac":{}
			},
			"metadataFormat":{
				"marc21":{
					"title":"245$a",
					"author":"100$a",
					"identifier":"001",
					"isbn":"020$a",
					"issn":"022$a",
					"edition":"250$a",
					"subtitle":"245$b"
				}
			}
		},
		"holdingsPolicy":{
			"locations":[{"code":"MAIN","name":"Main Library","supplyPreference":100}],
			"shelvingLocations":[{"code":"STACKS","name":"Stacks","supplyPreference":50}],
			"locationPolicies":[{"locationCode":"MAIN","shelvingLocationCode":"RARE","supplyPreference":-1}],
			"itemLoanPolicies":[{"code":"NORMAL","name":"Normal loan","lendable":true}]
		}
	}`

	res, data := jsonReq(t, http.MethodPost, "/entries", body, headers)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected POST status %d, got %d and body %s", http.StatusCreated, res.StatusCode, data)
	}

	var created struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal([]byte(data), &created); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}

	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	var entry map[string]any
	entry = make(map[string]any)
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry response: %v", err)
	}
	if entry["email"] != "contact@example.org" || entry["fromEmail"] != "from@example.org" || entry["tenant"] != "contract-tenant" || entry["vendor"] != "CrossLink" || entry["description"] != "Contract description" || entry["hrid"] != "contract-hrid" {
		t.Fatalf("new entry fields not preserved: %#v", entry)
	}
	illConfig := entry["illConfig"].(map[string]any)
	lender := illConfig["lendersOfLastResort"].([]any)[0].(map[string]any)
	if lender["authority"] != "ISIL" || lender["symbol"] != "CONTRACT-LOR" {
		t.Fatalf("illConfig.lendersOfLastResort did not round-trip as Symbol array: %#v", lender)
	}
	if illConfig["iso18626Url"] != "https://iso.example.org/iso18626" ||
		illConfig["iso18626Vendor"] != "ReShare" ||
		illConfig["includeRequestingAgencyInfo"] != true ||
		illConfig["includeSupplierInfo"] != false ||
		illConfig["includeReturnInfo"] != true ||
		illConfig["includeVendorNote"] != false ||
		illConfig["useOfferedCosts"] != true ||
		illConfig["noteFieldSeparator"] != " | " ||
		illConfig["supplierPatronPattern"] != "PATRON-{requesterSymbol}" ||
		illConfig["duplicateCheckWindowHours"] != float64(24) {
		t.Fatalf("illConfig fields did not round-trip: %#v", illConfig)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+created.Id, `{"illConfig":{"noteFieldSeparator":" / ","useOfferedCosts":false,"lendersOfLastResort":[]}}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected partial illConfig PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after illConfig PATCH status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	entry = make(map[string]any)
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry after illConfig PATCH: %v", err)
	}
	illConfig = entry["illConfig"].(map[string]any)
	if illConfig["noteFieldSeparator"] != " / " || illConfig["useOfferedCosts"] != false || illConfig["iso18626Url"] != "https://iso.example.org/iso18626" {
		t.Fatalf("partial illConfig PATCH did not merge fields: %#v", illConfig)
	}
	if _, ok := illConfig["lendersOfLastResort"]; ok {
		t.Fatalf("partial illConfig PATCH did not clear lendersOfLastResort: %#v", illConfig)
	}
	holdings := entry["catalogConfig"].(map[string]any)
	zoom := holdings["zoom"].(map[string]any)
	options := zoom["options"].(map[string]any)
	if holdings["metadataUpdateMode"] != "merge" ||
		zoom["address"] != "z3950.example.org:210/catalog" ||
		options["preferredRecordSyntax"] != "usmarc" ||
		options["count"] != "20" ||
		options["location"] != "STACKS" ||
		options["customOption"] != "preserved" {
		t.Fatalf("catalogConfig zoom fields did not round-trip: %#v", holdings)
	}
	policy := entry["holdingsPolicy"].(map[string]any)
	locations := policy["locations"].([]any)
	itemLoanPolicies := policy["itemLoanPolicies"].([]any)
	if locations[0].(map[string]any)["code"] != "MAIN" || itemLoanPolicies[0].(map[string]any)["lendable"] != true {
		t.Fatalf("holdingsPolicy did not round-trip: %#v", policy)
	}
	queryConfig := holdings["queryConfig"].(map[string]any)
	if queryConfig["type"] != "cql" || queryConfig["identifier"] != "rec.id = {term}" {
		t.Fatalf("catalogConfig queryConfig did not round-trip: %#v", queryConfig)
	}
	metadataMarc := holdings["metadataFormat"].(map[string]any)["marc21"].(map[string]any)
	if metadataMarc["title"] != "245$a" || metadataMarc["author"] != "100$a" {
		t.Fatalf("catalogConfig metadataFormat did not round-trip: %#v", metadataMarc)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+created.Id, `{
		"catalogConfig":{
			"zoom":{"options":{"count":"50","emptyValue":"","customOption":null,"missingOption":null}},
			"queryConfig":{"title":"new title query"},
			"holdingsFormat":{"marc":{"mainField":"998"}},
			"metadataFormat":{"marc21":{"title":"246$a"}}
		}
	}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected partial catalogConfig PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after catalogConfig PATCH status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	entry = make(map[string]any)
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry after catalogConfig PATCH: %v", err)
	}
	holdings = entry["catalogConfig"].(map[string]any)
	zoom = holdings["zoom"].(map[string]any)
	options = zoom["options"].(map[string]any)
	queryConfig = holdings["queryConfig"].(map[string]any)
	marcHoldings := holdings["holdingsFormat"].(map[string]any)["marc"].(map[string]any)
	metadataMarc = holdings["metadataFormat"].(map[string]any)["marc21"].(map[string]any)
	if holdings["metadataUpdateMode"] != "merge" || zoom["address"] != "z3950.example.org:210/catalog" ||
		options["count"] != "50" || options["preferredRecordSyntax"] != "usmarc" || options["emptyValue"] != "" ||
		queryConfig["title"] != "new title query" || queryConfig["identifier"] != "rec.id = {term}" ||
		marcHoldings["mainField"] != "998" || marcHoldings["locationSubField"] != "l" ||
		metadataMarc["title"] != "246$a" || metadataMarc["author"] != "100$a" {
		t.Fatalf("partial catalogConfig PATCH did not recursively merge fields: %#v", holdings)
	}
	if _, ok := options["customOption"]; ok {
		t.Fatalf("partial catalogConfig PATCH did not remove customOption: %#v", options)
	}
	if _, ok := options["missingOption"]; ok {
		t.Fatalf("partial catalogConfig PATCH added a null missingOption: %#v", options)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+created.Id, `{"holdingsPolicy":{"itemLoanPolicies":[{"code":"REFERENCE","name":"Reference only","lendable":false}]}}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected holdingsPolicy PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after holdingsPolicy PATCH status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	entry = make(map[string]any)
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry after holdingsPolicy PATCH: %v", err)
	}
	policy = entry["holdingsPolicy"].(map[string]any)
	itemLoanPolicies = policy["itemLoanPolicies"].([]any)
	locations = policy["locations"].([]any)
	if itemLoanPolicies[0].(map[string]any)["code"] != "REFERENCE" || itemLoanPolicies[0].(map[string]any)["lendable"] != false ||
		locations[0].(map[string]any)["code"] != "MAIN" {
		t.Fatalf("holdingsPolicy PATCH did not merge independent arrays: %#v", policy)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+created.Id, `{"catalogConfig":null,"holdingsPolicy":null}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected catalogConfig null PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after catalogConfig clear status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	if strings.Contains(data, `"catalogConfig"`) {
		t.Fatalf("catalogConfig should be omitted after nullable PATCH clear: %s", data)
	}
	if strings.Contains(data, `"holdingsPolicy"`) {
		t.Fatalf("holdingsPolicy should be omitted after nullable PATCH clear: %s", data)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+created.Id, `{
		"catalogConfig":{"queryConfig":{"title":"title = {term}"}},
		"illConfig":{"useOfferedCosts":false},
		"holdingsPolicy":{"locations":[]}
	}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected partial configuration creation status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after partial configuration creation status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	entry = make(map[string]any)
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry after partial configuration creation: %v", err)
	}
	if entry["catalogConfig"].(map[string]any)["queryConfig"].(map[string]any)["title"] != "title = {term}" ||
		entry["illConfig"].(map[string]any)["useOfferedCosts"] != false {
		t.Fatalf("partial PATCH did not create missing configurations: %#v", entry)
	}
	createdPolicy := entry["holdingsPolicy"].(map[string]any)
	if createdLocations, ok := createdPolicy["locations"].([]any); !ok || len(createdLocations) != 0 {
		t.Fatalf("partial PATCH did not create an explicitly empty holdingsPolicy locations array: %#v", createdPolicy)
	}
}

func TestPatchCatalogConfigRequiresAddressForCreation(t *testing.T) {
	resetDb()

	const entryID = "00000000-0000-0000-0000-000000000003"
	headers := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	tests := []struct {
		name        string
		body        string
		errorDetail string
	}{
		{
			name:        "SRU field without address",
			body:        `{"catalogConfig":{"sru":{"recordSchema":"marcxml"}}}`,
			errorDetail: "catalogConfig.sru.address is required when creating SRU configuration",
		},
		{
			name:        "empty SRU config",
			body:        `{"catalogConfig":{"sru":{}}}`,
			errorDetail: "catalogConfig.sru.address is required when creating SRU configuration",
		},
		{
			name:        "ZOOM field without address",
			body:        `{"catalogConfig":{"zoom":{"options":{"count":"10"}}}}`,
			errorDetail: "catalogConfig.zoom.address is required when creating ZOOM configuration",
		},
		{
			name:        "empty ZOOM config",
			body:        `{"catalogConfig":{"zoom":{}}}`,
			errorDetail: "catalogConfig.zoom.address is required when creating ZOOM configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, data := jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, tt.body, headers)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected incomplete catalogConfig PATCH status %d, got %d and body %s", http.StatusBadRequest, res.StatusCode, data)
			}
			if !strings.Contains(data, tt.errorDetail) {
				t.Fatalf("expected error %q, got %s", tt.errorDetail, data)
			}
		})
	}

	var count int
	if err := dbpool.QueryRow(context.Background(), "SELECT count(*) FROM catalog_configs WHERE entry = $1", entryID).Scan(&count); err != nil {
		t.Fatalf("failed to count catalog configs after rejected patches: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected patches created a catalog config, got count %d", count)
	}

	res, data := jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, `{
		"catalogConfig":{
			"sru":{"address":"https://catalog.example/sru","recordSchema":"marcxml"},
			"zoom":{"address":"catalog.example:210/db","options":{"count":"10"}}
		}
	}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected addressed catalogConfig creation status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, `{
		"catalogConfig":{
			"sru":{"recordSchema":"mods"},
			"zoom":{"options":{"count":"20"}}
		}
	}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected address-omitting update status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}

	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+entryID, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after catalogConfig updates status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry after catalogConfig updates: %v", err)
	}
	catalogConfig := entry["catalogConfig"].(map[string]any)
	sru := catalogConfig["sru"].(map[string]any)
	zoom := catalogConfig["zoom"].(map[string]any)
	options := zoom["options"].(map[string]any)
	if sru["address"] != "https://catalog.example/sru" || sru["recordSchema"] != "mods" ||
		zoom["address"] != "catalog.example:210/db" || options["count"] != "20" {
		t.Fatalf("catalogConfig creation or partial update did not round-trip: %#v", catalogConfig)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+entryID, `{
		"catalogConfig":{"zoom":{"options":{"count":null,"missingOption":null}}}
	}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected final ZOOM option deletion status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+entryID, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after final ZOOM option deletion status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	entry = make(map[string]any)
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry after final ZOOM option deletion: %v", err)
	}
	options = entry["catalogConfig"].(map[string]any)["zoom"].(map[string]any)["options"].(map[string]any)
	if len(options) != 0 {
		t.Fatalf("expected final ZOOM option deletion to persist an empty object, got %#v", options)
	}
}

func TestEntrySystemReadAndSymbolTenantCQL(t *testing.T) {
	resetDb()

	_, err := dbpool.Exec(
		context.Background(),
		"UPDATE entries SET tenant = $1 WHERE id = $2",
		"tenant-a",
		"00000000-0000-0000-0000-000000000002",
	)
	if err != nil {
		t.Fatalf("failed to seed tenant: %v", err)
	}

	headers := map[string]string{
		"X-Okapi-Permissions": `["directory.system.all"]`,
	}
	query := url.QueryEscape(`symbol any "TEST:ANINST" and tenant="tenant-a"`)
	res, data := jsonReq(t, http.MethodGet, "/entries?cql="+query+"&limit=1", "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected system CQL read status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}

	var response map[string]any
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		t.Fatalf("failed to parse entries response: %v", err)
	}
	about := response["about"].(map[string]any)
	items := response["items"].([]any)
	if about["count"] != float64(1) || len(items) != 1 {
		t.Fatalf("expected one CQL match with pagination count, got about=%#v items=%#v", about, items)
	}
	item := items[0].(map[string]any)
	if item["id"] != "00000000-0000-0000-0000-000000000002" || item["tenant"] != "tenant-a" {
		t.Fatalf("unexpected system CQL entry: %#v", item)
	}
}

func TestEntryCQLIncludesChildrenOfMatches(t *testing.T) {
	resetDb()

	const parentID = "00000000-0000-0000-0000-000000000002"
	ctx := context.Background()
	_, err := dbpool.Exec(ctx, "UPDATE entries SET tenant = 'tenant-a' WHERE id = $1", parentID)
	if err != nil {
		t.Fatalf("failed to seed parent tenant: %v", err)
	}
	_, err = dbpool.Exec(ctx, `
		INSERT INTO entries (id, parent, name, type, tenant) VALUES
			('01000000-0000-0000-0000-000000000001', $1, 'A Branch', 'Branch', 'other-tenant'),
			('01000000-0000-0000-0000-000000000002', $1, 'Z Branch', 'Branch', 'other-tenant')
	`, parentID)
	if err != nil {
		t.Fatalf("failed to seed branch entries: %v", err)
	}
	_, err = dbpool.Exec(ctx, `
		INSERT INTO symbols (owner, authority, symbol) VALUES
			('01000000-0000-0000-0000-000000000001', 'TEST', 'BRANCH'),
			('01000000-0000-0000-0000-000000000002', 'TEST', 'BRANCH2')
	`)
	if err != nil {
		t.Fatalf("failed to seed branch symbols: %v", err)
	}

	headers := map[string]string{
		"X-Okapi-Permissions": `["directory.system.all"]`,
	}
	type entriesResult struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
		About struct {
			Count int64 `json:"count"`
		} `json:"about"`
	}
	getEntries := func(t *testing.T, cql string, limit, offset int) entriesResult {
		t.Helper()
		endpoint := "/entries?cql=" + url.QueryEscape(cql) + "&limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset)
		res, data := jsonReq(t, http.MethodGet, endpoint, "", headers)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected CQL response status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
		}
		var result entriesResult
		if err := json.Unmarshal([]byte(data), &result); err != nil {
			t.Fatalf("failed to parse CQL response: %v", err)
		}
		return result
	}

	t.Run("children are counted sorted and paginated after expansion", func(t *testing.T) {
		result := getEntries(t, `symbol any "TEST:ANINST" and tenant="tenant-a"`, 2, 0)
		if result.About.Count != 3 || len(result.Items) != 2 ||
			result.Items[0].Name != "A Branch" || result.Items[1].Name != "An Institution" {
			t.Fatalf("unexpected first expanded page: %#v", result)
		}

		result = getEntries(t, `symbol any "TEST:ANINST" and tenant="tenant-a"`, 2, 2)
		if result.About.Count != 3 || len(result.Items) != 1 || result.Items[0].Name != "Z Branch" {
			t.Fatalf("unexpected second expanded page: %#v", result)
		}
	})

	t.Run("direct branch match does not include its parent", func(t *testing.T) {
		result := getEntries(t, `symbol any "TEST:BRANCH"`, 10, 0)
		if result.About.Count != 1 || len(result.Items) != 1 || result.Items[0].Name != "A Branch" {
			t.Fatalf("unexpected direct branch result: %#v", result)
		}
	})

	t.Run("direct and inherited matches are deduplicated", func(t *testing.T) {
		result := getEntries(t, `symbol any "TEST:ANINST TEST:BRANCH"`, 10, 0)
		if result.About.Count != 3 || len(result.Items) != 3 {
			t.Fatalf("expected parent and two unique branches, got %#v", result)
		}
	})

	t.Run("non-symbol CQL matches also include children", func(t *testing.T) {
		result := getEntries(t, `name="An Institution"`, 10, 0)
		if result.About.Count != 3 || len(result.Items) != 3 {
			t.Fatalf("expected name match and its branches, got %#v", result)
		}
	})
}

func TestPublicReadSanitizesProtectedLMSValues(t *testing.T) {
	resetDb()

	headers := map[string]string{
		"X-Okapi-Tenant":      "PUBLIC",
		"X-Okapi-Permissions": `["directory.public.all"]`,
	}
	res, data := jsonReq(t, http.MethodGet, "/entries/by-id/00000000-0000-0000-0000-000000000002", "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected public GET status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse public entry response: %v", err)
	}
	lmsConfig := entry["lmsConfig"].(map[string]any)
	if lmsConfig["fromAgencyAuthentication"] != "" {
		t.Fatalf("protected lmsConfig.fromAgencyAuthentication should be sanitized, got %#v", lmsConfig["fromAgencyAuthentication"])
	}
}
