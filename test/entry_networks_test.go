package test

import (
	"net/http"
	"testing"
)

func TestEntryNetworksCases(t *testing.T) {
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
			name:        "GET all entry networks",
			method:      http.MethodGet,
			endpoint:    "/entry-networks",
			status:      http.StatusOK,
			resFile:     "entry-networks.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET all entry networks with a specific network",
			method:      http.MethodGet,
			endpoint:    "/entry-networks?q=network=20000000-0000-0000-0000-000000000003",
			status:      http.StatusOK,
			resFile:     "entry-networks-filter.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET an entry network",
			method:      http.MethodGet,
			endpoint:    "/entry-networks/40000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "entry-network.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST entry network",
			method:          http.MethodPost,
			endpoint:        "/entry-networks",
			status:          http.StatusCreated,
			bodyFile:        "entry-network.post.req.json",
			refetchEndpoint: "/entry-networks",
			refetchFile:     "entry-network.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:        "POST entry network without network",
			method:      http.MethodPost,
			endpoint:    "/entry-networks",
			body:        `{"entry":"00000000-0000-0000-0000-000000000001"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:          "DELETE network membership",
			method:        http.MethodDelete,
			endpoint:      "/entry-networks/40000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}

	testCases(t, cases)
}
