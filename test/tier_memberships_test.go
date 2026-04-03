package test

import (
	"net/http"
	"testing"
)

func TestTierMembershipCases(t *testing.T) {
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
			name:        "GET all tier memberships",
			method:      http.MethodGet,
			endpoint:    "/tier-memberships",
			status:      http.StatusOK,
			resFile:     "tier-memberships.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a tier membership",
			method:      http.MethodGet,
			endpoint:    "/tier-memberships/00000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "tier-membership.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST tier membership",
			method:          http.MethodPost,
			endpoint:        "/tier-memberships",
			status:          http.StatusCreated,
			bodyFile:        "tier-membership.post.req.json",
			refetchEndpoint: "/tier-memberships",
			refetchFile:     "tier-membership.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:          "DELETE tier membership",
			method:        http.MethodDelete,
			endpoint:      "/tier-memberships/00000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}

	testCases(t, cases)
}
