package test

import (
	"net/http"
	"testing"
)

func TestTierCases(t *testing.T) {
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
			name:        "GET all tiers",
			method:      http.MethodGet,
			endpoint:    "/tiers",
			status:      http.StatusOK,
			resFile:     "tiers.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a tier",
			method:      http.MethodGet,
			endpoint:    "/tiers/30000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "tier.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST tier",
			method:          http.MethodPost,
			endpoint:        "/tiers",
			status:          http.StatusCreated,
			bodyFile:        "tier.post.req.json",
			refetchEndpoint: "/tiers",
			refetchFile:     "tier.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:          "DELETE tier",
			method:        http.MethodDelete,
			endpoint:      "/tiers/30000000-0000-0000-0000-000000000002",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}
	testCases(t, cases)
}
