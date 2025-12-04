package test

import (
	"context"
	"net/http"
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
			endpoint:    "/entries?q=name%3DAn%20Institution",
			status:      http.StatusOK,
			resFile:     "entries-cql-name.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with CQL wildcard query by description",
			method:      http.MethodGet,
			endpoint:    "/entries?q=description%3D%2Aparticular%2A",
			status:      http.StatusOK,
			resFile:     "entries-cql-desc.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entries with invalid CQL query",
			method:      http.MethodGet,
			endpoint:    "/entries?q=invalid%28%28%28",
			status:      http.StatusBadRequest,
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
	}
	testCases(t, cases)
}
