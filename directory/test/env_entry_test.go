package test

import (
	"net/http"
	"strings"
	"testing"
)

func TestEntryOwnershipUsesTenantRatherThanSymbol(t *testing.T) {
	shooInstitutionHeaders := map[string]string{
		"X-Okapi-Tenant":      "SHOO",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	testInstitutionHeaders := map[string]string{
		"X-Okapi-Tenant":      "TEST",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	resetDb()
	postRes, postData := jsonReq(t, http.MethodPost, "/entries", `{
		"name":"Tenant-Owned Institution",
		"tenant":"SHOO",
		"symbols":[{"authority":"GRONK","symbol":"NOT-SHOO"}],
		"lmsConfig":{
			"address":"https://lms.example.org",
			"fromAgency":"shoo",
			"fromAgencyAuthentication":"secret"
		}
	}`, consortiumPermissionHeaders)
	if postRes.StatusCode != http.StatusCreated {
		t.Fatalf("POST failed: %d %s", postRes.StatusCode, postData)
	}

	const endpoint = "/entries/by-symbol/GRONK:NOT-SHOO"
	getRes, getData := jsonReq(t, http.MethodGet, endpoint, "", shooInstitutionHeaders)
	if getRes.StatusCode != http.StatusOK || !strings.Contains(getData, `"fromAgencyAuthentication":"secret"`) {
		t.Fatalf("tenant owner did not receive protected data: %d %s", getRes.StatusCode, getData)
	}

	nonOwnerGetRes, nonOwnerGetData := jsonReq(t, http.MethodGet, endpoint, "", testInstitutionHeaders)
	if nonOwnerGetRes.StatusCode != http.StatusOK || strings.Contains(nonOwnerGetData, `"fromAgencyAuthentication":"secret"`) {
		t.Fatalf("non-owner received protected data: %d %s", nonOwnerGetRes.StatusCode, nonOwnerGetData)
	}

	patchRes, patchData := jsonReq(t, http.MethodPatch, endpoint, `{"name":"Tenant Owner Updated"}`, shooInstitutionHeaders)
	if patchRes.StatusCode != http.StatusNoContent {
		t.Fatalf("tenant-owner PATCH failed: %d %s", patchRes.StatusCode, patchData)
	}

	patchRes, patchData = jsonReq(t, http.MethodPatch, endpoint, `{"name":"Non-owner Updated"}`, testInstitutionHeaders)
	if patchRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("non-owner PATCH expected 401: %d %s", patchRes.StatusCode, patchData)
	}
}
