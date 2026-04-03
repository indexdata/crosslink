package test

import (
	"net/http"
	"testing"
)

func TestNetworkCases(t *testing.T) {
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
			name:        "GET all networks",
			method:      http.MethodGet,
			endpoint:    "/networks",
			status:      http.StatusOK,
			resFile:     "networks.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a network",
			method:      http.MethodGet,
			endpoint:    "/networks/00000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "network.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST network",
			method:          http.MethodPost,
			endpoint:        "/networks",
			status:          http.StatusCreated,
			bodyFile:        "network.post.req.json",
			refetchEndpoint: "/networks",
			refetchFile:     "network.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:          "DELETE network",
			method:        http.MethodDelete,
			endpoint:      "/networks/00000000-0000-0000-0000-000000000002",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}
	testCases(t, cases)
}
