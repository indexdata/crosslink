package test

import (
	"net/http"
	"testing"
)

func TestMembershipCases(t *testing.T) {
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
			name:        "GET all memberships",
			method:      http.MethodGet,
			endpoint:    "/memberships",
			status:      http.StatusOK,
			resFile:     "memberships.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a membership",
			method:      http.MethodGet,
			endpoint:    "/memberships/00000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "membership.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST membership",
			method:          http.MethodPost,
			endpoint:        "/memberships",
			status:          http.StatusCreated,
			bodyFile:        "membership.post.req.json",
			refetchEndpoint: "/memberships",
			refetchFile:     "membership.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:          "DELETE membership",
			method:        http.MethodDelete,
			endpoint:      "/memberships/00000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}
	testCases(t, cases)
}
