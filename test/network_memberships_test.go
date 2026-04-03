package test

import (
	"net/http"
	"testing"
)

func TestNetworkMembershipCases(t *testing.T) {
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
			name:        "GET all network memberships",
			method:      http.MethodGet,
			endpoint:    "/network-memberships",
			status:      http.StatusOK,
			resFile:     "network-memberships.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a network membership",
			method:      http.MethodGet,
			endpoint:    "/network-memberships/00000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "network-membership.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST network membership",
			method:          http.MethodPost,
			endpoint:        "/network-memberships",
			status:          http.StatusCreated,
			bodyFile:        "network-membership.post.req.json",
			refetchEndpoint: "/network-memberships",
			refetchFile:     "network-membership.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:        "POST network membership without network",
			method:      http.MethodPost,
			endpoint:    "/network-memberships",
			body:        `{"membership":"00000000-0000-0000-0000-000000000001"}`,
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:          "DELETE network membership",
			method:        http.MethodDelete,
			endpoint:      "/network-memberships/00000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}

	testCases(t, cases)
}
