package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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
			resFile:     "entries-cql-type.get.res.json",
			addlHeaders: institutionPermissionHeaders,
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
		"UPDATE entries SET lender_of_last_resort = ARRAY[$1]::text[] WHERE id = $2",
		"TEST:PATCHED-LOR",
		"00000000-0000-0000-0000-000000000002",
	)
	if err != nil {
		t.Fatalf("failed to seed lender_of_last_resort: %v", err)
	}

	res, data := jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002", `{"lenderOfLastResort":null}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected PATCH response status of %d, got %d and body of %s", http.StatusNoContent, res.StatusCode, data)
	}

	var lenderOfLastResort []string
	err = dbpool.QueryRow(
		context.Background(),
		"SELECT lender_of_last_resort FROM entries WHERE id = $1",
		"00000000-0000-0000-0000-000000000002",
	).Scan(&lenderOfLastResort)
	if err != nil {
		t.Fatalf("failed to fetch lender_of_last_resort: %v", err)
	}
	if lenderOfLastResort != nil {
		t.Fatalf("expected lender_of_last_resort to be null, got %q", lenderOfLastResort)
	}
}

func TestEntryDirectoryContractFieldsAndHoldingsConfig(t *testing.T) {
	resetDb()

	headers := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	body := `{
		"name":"Contract Test Entry",
		"type":"Institution",
		"parent":"00000000-0000-0000-0000-000000000004",
		"fromEmail":"from@example.org",
		"tenant":"contract-tenant",
		"vendor":"CrossLink",
		"lenderOfLastResort":[{"authority":"ISIL","symbol":"CONTRACT-LOR"}],
		"symbols":[{"authority":"ISIL","symbol":"CONTRACT"}],
		"holdingsConfig":{
			"metadataUpdateMode":"merge",
			"zoom":{
				"address":"z3950.example.org:210/catalog",
				"options":{
					"preferredRecordSyntax":"usmarc",
					"count":"20",
					"location":"STACKS"
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
	if strings.Contains(data, `"email"`) {
		t.Fatalf("entry response must not contain removed email field: %s", data)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatalf("failed to parse entry response: %v", err)
	}
	if entry["fromEmail"] != "from@example.org" || entry["tenant"] != "contract-tenant" || entry["vendor"] != "CrossLink" {
		t.Fatalf("new entry fields not preserved: %#v", entry)
	}
	lender := entry["lenderOfLastResort"].([]any)[0].(map[string]any)
	if lender["authority"] != "ISIL" || lender["symbol"] != "CONTRACT-LOR" {
		t.Fatalf("lenderOfLastResort did not round-trip as Symbol array: %#v", lender)
	}
	holdings := entry["holdingsConfig"].(map[string]any)
	zoom := holdings["zoom"].(map[string]any)
	options := zoom["options"].(map[string]any)
	if holdings["metadataUpdateMode"] != "merge" ||
		zoom["address"] != "z3950.example.org:210/catalog" ||
		options["preferredRecordSyntax"] != "usmarc" ||
		options["count"] != "20" ||
		options["location"] != "STACKS" {
		t.Fatalf("holdingsConfig zoom fields did not round-trip: %#v", holdings)
	}
	queryConfig := holdings["queryConfig"].(map[string]any)
	if queryConfig["type"] != "cql" || queryConfig["identifier"] != "rec.id = {term}" {
		t.Fatalf("holdingsConfig queryConfig did not round-trip: %#v", queryConfig)
	}
	metadataMarc := holdings["metadataFormat"].(map[string]any)["marc21"].(map[string]any)
	if metadataMarc["title"] != "245$a" || metadataMarc["author"] != "100$a" {
		t.Fatalf("holdingsConfig metadataFormat did not round-trip: %#v", metadataMarc)
	}

	res, data = jsonReq(t, http.MethodPatch, "/entries/by-id/"+created.Id, `{"holdingsConfig":null}`, headers)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected holdingsConfig null PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, "/entries/by-id/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET after holdingsConfig clear status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	if strings.Contains(data, `"holdingsConfig"`) {
		t.Fatalf("holdingsConfig should be omitted after nullable PATCH clear: %s", data)
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
