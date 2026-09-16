package test

import (
	"encoding/json"
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
			endpoint:    "/networks/20000000-0000-0000-0000-000000000001",
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
			name:        "POST network missing consortium",
			method:      http.MethodPost,
			endpoint:    "/networks",
			status:      http.StatusBadRequest,
			body:        `{"name":"No Consortium Network"}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST network with non-consortium entry",
			method:      http.MethodPost,
			endpoint:    "/networks",
			status:      http.StatusBadRequest,
			body:        `{"name":"Institution Network","consortium":"00000000-0000-0000-0000-000000000002"}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST network without name",
			method:      http.MethodPost,
			endpoint:    "/networks",
			status:      http.StatusBadRequest,
			body:        `{"consortium":"00000000-0000-0000-0000-000000000004","priority":0}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST network with blank name",
			method:      http.MethodPost,
			endpoint:    "/networks",
			status:      http.StatusBadRequest,
			body:        `{"name":"   ","consortium":"00000000-0000-0000-0000-000000000004","priority":0}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "POST network with duplicate name",
			method:      http.MethodPost,
			endpoint:    "/networks",
			status:      http.StatusConflict,
			body:        `{"name":"The Ultimate Network","consortium":"00000000-0000-0000-0000-000000000004","priority":0}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH network",
			method:      http.MethodPatch,
			endpoint:    "/networks/20000000-0000-0000-0000-000000000001",
			status:      http.StatusNoContent,
			bodyFile:    "network.patch.req.json",
			refetchFile: "network.patch.refetch.json",
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH network not found",
			method:      http.MethodPatch,
			endpoint:    "/networks/ffffffff-ffff-ffff-ffff-ffffffffffff",
			status:      http.StatusNotFound,
			body:        `{"reciprocal":true}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH network rejects name",
			method:      http.MethodPatch,
			endpoint:    "/networks/20000000-0000-0000-0000-000000000002",
			status:      http.StatusBadRequest,
			body:        `{"name":"The Ultimate Network"}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH network rejects consortium",
			method:      http.MethodPatch,
			endpoint:    "/networks/20000000-0000-0000-0000-000000000002",
			status:      http.StatusBadRequest,
			body:        `{"consortium":"00000000-0000-0000-0000-000000000004"}`,
			addlHeaders: consortiumPermissionHeaders,
		},
		{
			name:        "PATCH network without permission",
			method:      http.MethodPatch,
			endpoint:    "/networks/20000000-0000-0000-0000-000000000001",
			status:      http.StatusUnauthorized,
			body:        `{"reciprocal":true}`,
			addlHeaders: institutionPermissionHeaders,
		},
		{
			name:          "DELETE network",
			method:        http.MethodDelete,
			endpoint:      "/networks/20000000-0000-0000-0000-000000000002",
			status:        http.StatusNoContent,
			refetchStatus: http.StatusNotFound,
			addlHeaders:   consortiumPermissionHeaders,
		},
	}
	testCases(t, cases)
}

func TestNetworkReciprocalCreateAndRead(t *testing.T) {
	resetDb()

	headers := map[string]string{
		"X-Okapi-Tenant":      "ANINST",
		"X-Okapi-Permissions": `["directory.consortium.all"]`,
	}
	body := `{
		"name":"Reciprocal Test Network",
		"consortium":"00000000-0000-0000-0000-000000000004",
		"reciprocal":true
	}`

	res, data := jsonReq(t, http.MethodPost, "/networks", body, headers)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected POST status %d, got %d and body %s", http.StatusCreated, res.StatusCode, data)
	}
	var created struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal([]byte(data), &created); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}

	res, data = jsonReq(t, http.MethodGet, "/networks/"+created.Id, "", headers)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	var network map[string]any
	if err := json.Unmarshal([]byte(data), &network); err != nil {
		t.Fatalf("failed to parse network response: %v", err)
	}
	if network["reciprocal"] != true {
		t.Fatalf("network reciprocal did not round-trip as true: %#v", network)
	}
}

func TestNetworkPatchPreservesOmittedFieldsAndClearsReciprocal(t *testing.T) {
	resetDb()

	endpoint := "/networks/20000000-0000-0000-0000-000000000001"
	for _, body := range []string{
		`{"reciprocal":true}`,
		`{}`,
	} {
		res, data := jsonReq(t, http.MethodPatch, endpoint, body, standardHeaders)
		if res.StatusCode != http.StatusNoContent {
			t.Fatalf("expected PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
		}
	}

	res, data := jsonReq(t, http.MethodGet, endpoint, "", standardHeaders)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	var network map[string]any
	if err := json.Unmarshal([]byte(data), &network); err != nil {
		t.Fatalf("failed to parse network response: %v", err)
	}
	if network["name"] != "The Ultimate Network" || network["reciprocal"] != true || network["consortium"] != "00000000-0000-0000-0000-000000000004" {
		t.Fatalf("network patch did not preserve omitted fields: %#v", network)
	}

	res, data = jsonReq(t, http.MethodPatch, endpoint, `{"reciprocal":null}`, standardHeaders)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected nullable PATCH status %d, got %d and body %s", http.StatusNoContent, res.StatusCode, data)
	}
	res, data = jsonReq(t, http.MethodGet, endpoint, "", standardHeaders)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected GET status %d, got %d and body %s", http.StatusOK, res.StatusCode, data)
	}
	network = nil
	if err := json.Unmarshal([]byte(data), &network); err != nil {
		t.Fatalf("failed to parse network response: %v", err)
	}
	if _, present := network["reciprocal"]; present {
		t.Fatalf("network reciprocal was not cleared: %#v", network)
	}
}
