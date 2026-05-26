package test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kinbiko/jsonassert"
)

func TestTiersForEntry(t *testing.T) {
	institutionPermissionHeaders := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.institution.all"]`,
	}

	cases := []httpTestCase{
		{
			name:        "GET tiers for entry by id",
			method:      http.MethodGet,
			endpoint:    "/entries/by-id/00000000-0000-0000-0000-000000000001/tiers",
			status:      http.StatusOK,
			resFile:     "entry-tiers-for-entry.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:        "GET tiers for entry by symbol",
			method:      http.MethodGet,
			endpoint:    "/entries/by-symbol/TEST:ANINST/tiers",
			status:      http.StatusOK,
			resFile:     "entry-tiers-for-entry-empty.get.res.json",
			addlHeaders: institutionPermissionHeaders,
		},
	}

	testCases(t, cases)
}

func TestAddTierForEntryByID(t *testing.T) {
	resetDb()
	ja := jsonassert.New(t)

	consortiumPermissionHeaders := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	postTierForEntry(t, "/entries/by-id/00000000-0000-0000-0000-000000000001/tiers", consortiumPermissionHeaders)
	assertJSONFixture(t, ja, http.MethodGet, "/entries/by-id/00000000-0000-0000-0000-000000000001/tiers", "", consortiumPermissionHeaders, http.StatusOK, "entry-tiers-for-entry.post.refetch.json")
}

func TestDeleteTierForEntryByID(t *testing.T) {
	resetDb()
	ja := jsonassert.New(t)

	consortiumPermissionHeaders := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	endpoint := "/entries/by-id/00000000-0000-0000-0000-000000000001/tiers"
	deleteEndpoint := endpoint + "/30000000-0000-0000-0000-000000000001"

	res, data := jsonReq(t, http.MethodDelete, deleteEndpoint, "", consortiumPermissionHeaders)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("Expected DELETE response status of %d, got %d and body of %s", http.StatusNoContent, res.StatusCode, data)
	}

	assertJSONFixture(t, ja, http.MethodGet, endpoint, "", consortiumPermissionHeaders, http.StatusOK, "entry-tiers-for-entry-empty.get.res.json")
}

func TestAddAndDeleteTierForEntryBySymbol(t *testing.T) {
	resetDb()
	ja := jsonassert.New(t)

	consortiumPermissionHeaders := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}

	endpoint := "/entries/by-symbol/TEST:ANINST/tiers"

	postTierForEntry(t, endpoint, consortiumPermissionHeaders)
	assertJSONFixture(t, ja, http.MethodGet, endpoint, "", consortiumPermissionHeaders, http.StatusOK, "entry-tiers-for-entry-middle.get.res.json")

	deleteEndpoint := endpoint + "/30000000-0000-0000-0000-000000000002"
	res, data := jsonReq(t, http.MethodDelete, deleteEndpoint, "", consortiumPermissionHeaders)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("Expected DELETE response status of %d, got %d and body of %s", http.StatusNoContent, res.StatusCode, data)
	}

	assertJSONFixture(t, ja, http.MethodGet, endpoint, "", consortiumPermissionHeaders, http.StatusOK, "entry-tiers-for-entry-empty.get.res.json")
}

func postTierForEntry(t *testing.T, endpoint string, headers map[string]string) {
	t.Helper()

	res, data := jsonReq(t, http.MethodPost, endpoint, `{"id":"30000000-0000-0000-0000-000000000002"}`, headers)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("Expected POST response status of %d, got %d and body of %s", http.StatusCreated, res.StatusCode, data)
	}

	var postResult map[string]any
	if err := json.Unmarshal([]byte(data), &postResult); err != nil {
		t.Fatalf("Error parsing POST response to get ID: %v", err)
	}

	idOfPosted, ok := postResult["id"].(string)
	if !ok || len(idOfPosted) != 36 {
		t.Fatalf("Did not find a 36 character ID property, instead got: %v", postResult["id"])
	}
}

func assertJSONFixture(t *testing.T, ja *jsonassert.Asserter, method string, endpoint string, body string, headers map[string]string, status int, fixture string) {
	t.Helper()

	res, data := jsonReq(t, method, endpoint, body, headers)
	if res.StatusCode != status {
		t.Fatalf("Expected response status of %d, got %d and body of %s", status, res.StatusCode, data)
	}

	expected, err := loadApiFixture(fixture)
	if err != nil {
		t.Fatalf("Error loading expected fixture %s: %v", fixture, err)
	}
	ja.Assertf(data, expected)
}
