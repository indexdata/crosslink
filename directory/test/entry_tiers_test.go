package test

import (
	"net/http"
	"testing"
)

func TestEntryTiers(t *testing.T) {
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
			name:        "GET all entry tiers",
			method:      http.MethodGet,
			endpoint:    "/entry-tiers",
			status:      http.StatusOK,
			resFile:     "entry-tiers.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET entry tiers filtered",
			method:      http.MethodGet,
			endpoint:    "/entry-tiers?q=tier=123",
			status:      http.StatusOK,
			resFile:     "entry-tiers-filtered.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a tier membership",
			method:      http.MethodGet,
			endpoint:    "/entry-tiers/50000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "entry-tier.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST tier membership",
			method:          http.MethodPost,
			endpoint:        "/entry-tiers",
			status:          http.StatusCreated,
			bodyFile:        "entry-tier.post.req.json",
			refetchEndpoint: "/entry-tiers",
			refetchFile:     "entry-tier.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:          "DELETE tier membership",
			method:        http.MethodDelete,
			endpoint:      "/entry-tiers/50000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}

	testCases(t, cases)
}
