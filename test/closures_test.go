package test

import (
	"net/http"
	"testing"
)

func TestClosureCases(t *testing.T) {
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
			name:        "GET all closures",
			method:      http.MethodGet,
			endpoint:    "/closures",
			status:      http.StatusOK,
			resFile:     "closures.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET a closure",
			method:      http.MethodGet,
			endpoint:    "/closures/00000000-0000-0000-0000-000000000001",
			status:      http.StatusOK,
			resFile:     "closure.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:            "POST closure",
			method:          http.MethodPost,
			endpoint:        "/closures",
			status:          http.StatusCreated,
			bodyFile:        "closure.post.req.json",
			refetchEndpoint: "/closures",
			refetchFile:     "closure.post.refetch.json",
			addlHeaders:     consortiumPermissionHeaders,
		},
		{
			name:        "POST closure with missing fields",
			method:      http.MethodPost,
			endpoint:    "/closures",
			bodyFile:    "closure-missing-fields.post.req.json",
			status:      http.StatusBadRequest,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH closure",
			method:      http.MethodPatch,
			endpoint:    "/closures/00000000-0000-0000-0000-000000000001",
			status:      http.StatusNoContent,
			bodyFile:    "closure.patch.req.json",
			refetchFile: "closure.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:          "DELETE closure",
			method:        http.MethodDelete,
			endpoint:      "/closures/00000000-0000-0000-0000-000000000001",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}

	testCases(t, cases)
}
